package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/feed"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// RematchOptions specifies the parameters for a rematch run.
type RematchOptions struct {
	IDs           []int
	AutoRescore   bool
	ForceAIEnrich bool
	JobID         int // when non-zero, caller has already created the job record and emitted the initial event
}

// RematchResult holds the outcome of a RunRematch call.
type RematchResult struct {
	Rematched       int
	NoLongerMatches int
	Rescored        int
	Skipped         int
	Updated         []models.StagedTorrent
}

// RematchDeps holds service dependencies for RunRematch.
type RematchDeps struct {
	Store     storage.Store
	Matcher   *matcher.Matcher
	Enricher  *ai.Enricher      // may be nil
	Scorer    *ai.Scorer        // may be nil
	Provider  ai.Provider       // may be nil — used for availability check
	LogBuffer *logbuffer.Buffer // may be nil
	Logger    *zap.Logger       // may be nil; falls back to nop
}

// RunRematch re-parses and re-matches the given torrents, optionally re-scoring
// items that still match. Items that no longer match any rule are transitioned
// to "rejected". Job lifecycle and SSE fan-out are handled internally.
func RunRematch(ctx context.Context, opts RematchOptions, deps RematchDeps) (RematchResult, error) {
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
		jobID, _ = deps.Store.CreateJob("rematch")
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitJobEvent(models.JobRecord{
				ID:        jobID,
				Type:      "rematch",
				Status:    "running",
				StartedAt: startedAt,
			})
		}
	}

	canRescore := opts.AutoRescore && deps.Scorer != nil && deps.Provider != nil && deps.Provider.Available()
	var history []models.Activity
	if canRescore {
		history, _ = deps.Store.GetActivity(50, 0, "")
	}

	var (
		result  RematchResult
		lastErr error
	)

	for _, id := range opts.IDs {
		if ctx.Err() != nil {
			break
		}
		t, err := deps.Store.GetByID(id)
		if err != nil || t == nil {
			result.Skipped++
			continue
		}
		if !isRematchEligible(t.Status) {
			result.Skipped++
			continue
		}

		item := t.FeedItem
		feed.ParseTitleMetadata(&item)
		if deps.Enricher != nil {
			if opts.ForceAIEnrich {
				deps.Enricher.EnrichForce(&item)
			} else {
				deps.Enricher.Enrich(&item)
			}
		}

		matches, reason := deps.Matcher.Match(item)
		newStatus := t.Status
		newReason := reason
		if matches {
			result.Rematched++
		} else {
			result.NoLongerMatches++
			newStatus = "rejected"
			if reason != "" {
				newReason = fmt.Sprintf("rematch: no longer matches current rules (%s)", reason)
			} else {
				newReason = "rematch: no longer matches current rules"
			}
		}

		if err := deps.Store.UpdateAfterRematch(t.ID, item, newReason, newStatus); err != nil {
			log.Error("failed to persist rematch update", zap.Int("id", t.ID), zap.Error(err))
			lastErr = err
			result.Skipped++
			continue
		}

		refreshed, err := deps.Store.GetByID(t.ID)
		if err != nil || refreshed == nil {
			result.Skipped++
			continue
		}

		if canRescore && matches {
			scored := deps.Scorer.ScoreAll([]models.StagedTorrent{*refreshed}, history)
			if len(scored) > 0 {
				r := scored[0]
				if err := deps.Store.UpdateAIScore(refreshed.ID, r.AIScore, r.AIReason, r.MatchConfidence, r.MatchConfidenceReason); err != nil {
					log.Error("failed to update AI score after rematch", zap.Int("id", refreshed.ID), zap.Error(err))
					lastErr = err
				} else {
					result.Rescored++
					refreshed.AIScore = r.AIScore
					refreshed.AIReason = r.AIReason
					refreshed.AIScored = true
					refreshed.MatchConfidence = r.MatchConfidence
					refreshed.MatchConfidenceReason = r.MatchConfidenceReason
				}
			}
		}

		result.Updated = append(result.Updated, *refreshed)
	}

	summary := models.JobSummary{
		ItemsFound:   len(opts.IDs),
		ItemsMatched: result.Rematched,
		ItemsScored:  result.Rescored,
	}
	now := time.Now()
	finalJob := models.JobRecord{
		ID:          jobID,
		Type:        "rematch",
		StartedAt:   startedAt,
		CompletedAt: &now,
		Summary:     summary,
	}
	if lastErr != nil && len(result.Updated) == 0 {
		finalJob.Status = "failed"
		finalJob.Summary.ErrorMessage = lastErr.Error()
		_ = deps.Store.FailJob(jobID, lastErr.Error())
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitAlertEvent(models.AlertRecord{
				Action:      "job_failed",
				Message:     "Rematch failed: " + lastErr.Error(),
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

	log.Info("torrents rematched",
		zap.Int("requested", len(opts.IDs)),
		zap.Int("rematched", result.Rematched),
		zap.Int("no_longer_matches", result.NoLongerMatches),
		zap.Int("rescored", result.Rescored),
		zap.Int("skipped", result.Skipped),
	)
	return result, lastErr
}

// isRematchEligible returns true for statuses that may be re-evaluated.
func isRematchEligible(status string) bool {
	switch status {
	case "pending", "accepted", "rejected":
		return true
	default:
		return false
	}
}
