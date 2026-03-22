package ops

import (
	"context"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// RescoreOptions specifies which torrents to re-score.
type RescoreOptions struct {
	IDs []int
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

	jobID, _ := deps.Store.CreateJob("rescore")
	startedAt := time.Now()
	if deps.LogBuffer != nil {
		deps.LogBuffer.EmitJobEvent(models.JobRecord{
			ID:        jobID,
			Type:      "rescore",
			Status:    "running",
			StartedAt: startedAt,
		})
	}

	history, _ := deps.Store.GetActivity(50, 0, "")

	var updated []models.StagedTorrent
	var lastErr error

	for _, id := range opts.IDs {
		if ctx.Err() != nil {
			break
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

	summary := models.JobSummary{ItemsScored: len(updated)}
	now := time.Now()
	finalJob := models.JobRecord{
		ID:          jobID,
		Type:        "rescore",
		StartedAt:   startedAt,
		CompletedAt: &now,
		Summary:     summary,
	}
	if lastErr != nil && len(updated) == 0 {
		finalJob.Status = "failed"
		finalJob.Summary.ErrorMessage = lastErr.Error()
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
	return updated, lastErr
}
