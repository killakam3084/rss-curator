package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// RescoreOptions specifies which torrents to re-score.
type RescoreOptions struct {
	IDs              []int
	JobID            int // when non-zero, caller has already created the job record and emitted the initial event
	ProgressInterval int // emit progress every N items; 0 or 1 = every item
}

// RescoreDeps holds service dependencies for RunRescore.
type RescoreDeps struct {
	Store     storage.Store
	Scorer    *ai.Scorer
	Provider  ai.Provider       // used for availability check
	LogBuffer *logbuffer.Buffer // may be nil
	Logger    *zap.Logger       // may be nil; falls back to nop
}

// RunRescore force re-scores the given torrents, bypassing the ai_scored gate
// used by the background backfill. It returns the updated torrent records and
// manages the "rescore" job lifecycle internally.
func RunRescore(ctx context.Context, opts RescoreOptions, deps RescoreDeps) ([]models.StagedTorrent, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}

	var jobID int
	startedAt := time.Now()
	if opts.JobID > 0 {
		// Caller pre-allocated the job record and already emitted the initial event.
		jobID = opts.JobID
	} else {
		jobID, _ = deps.Store.CreateJob("rescore")
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitJobEvent(models.JobRecord{
				ID:        jobID,
				Type:      "rescore",
				Status:    "running",
				StartedAt: startedAt,
			})
		}
	}

	history, _ := deps.Store.GetActivity(50, 0, "")

	var updated []models.StagedTorrent
	var lastErr error

	total := len(opts.IDs)
	interval := opts.ProgressInterval
	if interval <= 0 {
		interval = 1
	}
	for i, id := range opts.IDs {
		if ctx.Err() != nil {
			break
		}
		// Emit a progress event on the first item and every interval items.
		if deps.LogBuffer != nil && (i == 0 || (i+1)%interval == 0) {
			deps.LogBuffer.EmitJobEvent(models.JobRecord{
				ID:        jobID,
				Type:      "rescore",
				Status:    "running",
				StartedAt: startedAt,
				Progress:  fmt.Sprintf("%d / %d", i+1, total),
			})
		}
		t, err := deps.Store.GetByID(id)
		if err != nil || t == nil {
			continue
		}
		scored := deps.Scorer.ScoreAll([]models.StagedTorrent{*t}, history)
		if len(scored) == 0 {
			continue
		}
		r := scored[0]
		if err := deps.Store.UpdateAIScore(t.ID, r.AIScore, r.AIReason, r.MatchConfidence, r.MatchConfidenceReason); err != nil {
			log.Error("failed to update AI score", zap.Int("id", t.ID), zap.Error(err))
			lastErr = err
			continue
		}
		r.AIScored = true
		updated = append(updated, r)
	}

	summary := models.RescoreSummary{ItemsScored: len(updated)}
	now := time.Now()
	summaryJSON, _ := json.Marshal(summary)
	finalJob := models.JobRecord{
		ID:          jobID,
		Type:        "rescore",
		StartedAt:   startedAt,
		CompletedAt: &now,
		Summary:     summaryJSON,
	}
	if ctx.Err() != nil {
		finalJob.Status = "cancelled"
		_ = deps.Store.CancelJob(jobID, summary)
	} else if lastErr != nil && len(updated) == 0 {
		finalJob.Status = "failed"
		_ = deps.Store.FailJob(jobID, lastErr.Error())
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitAlertEvent(models.AlertRecord{
				Action:      "job_failed",
				Message:     "Rescore failed: " + lastErr.Error(),
				TriggeredAt: now,
			})
		}
	} else {
		finalJob.Status = "completed"
		_ = deps.Store.CompleteJob(jobID, summary)
	}
	if deps.LogBuffer != nil {
		deps.LogBuffer.EmitJobEvent(finalJob)
	}

	log.Info("torrents rescored", zap.Int("count", len(updated)))
	if ctx.Err() != nil {
		return updated, ctx.Err()
	}
	return updated, lastErr
}
