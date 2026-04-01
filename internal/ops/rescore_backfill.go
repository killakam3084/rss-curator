package ops

import (
	"context"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// RescoreBackfillDeps holds service dependencies for RunRescoreBackfill.
type RescoreBackfillDeps struct {
	Store      storage.Store
	Scorer     *ai.Scorer
	ScorerProv ai.Provider       // used for availability check
	LogBuffer  *logbuffer.Buffer // may be nil
	Logger     *zap.Logger       // may be nil; falls back to nop
}

// RunRescoreBackfill scores all staged torrents that have ai_scored=false.
//
// It is a no-op when the scorer is unavailable or when every item is already
// scored, making it safe to run on a regular schedule as a persistent safety
// net for items ingested while the AI provider was temporarily unavailable.
func RunRescoreBackfill(ctx context.Context, deps RescoreBackfillDeps) (models.JobSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}

	if deps.ScorerProv == nil || !deps.ScorerProv.Available() || deps.Scorer == nil {
		log.Debug("rescore_backfill: scorer unavailable, skipping")
		return models.JobSummary{}, nil
	}

	jobID, jobErr := deps.Store.CreateJob("rescore_backfill")
	if jobErr != nil {
		log.Warn("could not create rescore_backfill job", zap.Error(jobErr))
	}
	if deps.LogBuffer != nil && jobErr == nil {
		deps.LogBuffer.EmitJobEvent(models.JobRecord{
			ID:     jobID,
			Type:   "rescore_backfill",
			Status: "running",
		})
	}

	all, err := deps.Store.List("", "")
	if err != nil {
		log.Error("rescore_backfill: could not list torrents", zap.Error(err))
		if jobErr == nil {
			_ = deps.Store.FailJob(jobID, err.Error())
		}
		return models.JobSummary{}, err
	}

	history, _ := deps.Store.GetActivity(50, 0, "")
	backfilled := 0
	for _, t := range all {
		if ctx.Err() != nil {
			break
		}
		if t.AIScored {
			continue
		}
		scored := deps.Scorer.ScoreAll([]models.StagedTorrent{t}, history)
		if len(scored) > 0 {
			if err := deps.Store.UpdateAIScore(
				t.ID,
				scored[0].AIScore,
				scored[0].AIReason,
				scored[0].MatchConfidence,
				scored[0].MatchConfidenceReason,
			); err == nil {
				backfilled++
			}
		}
	}

	summary := models.JobSummary{ItemsScored: backfilled}
	log.Info("rescore_backfill complete", zap.Int("backfilled", backfilled))

	if jobErr == nil {
		if ctx.Err() != nil {
			_ = deps.Store.FailJob(jobID, "context cancelled")
		} else {
			_ = deps.Store.CompleteJob(jobID, summary)
		}
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitJobEvent(models.JobRecord{
				ID:      jobID,
				Type:    "rescore_backfill",
				Status:  "completed",
				Summary: summary,
			})
		}
	}

	return summary, nil
}
