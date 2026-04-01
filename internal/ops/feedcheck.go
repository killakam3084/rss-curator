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

// FeedCheckConfig holds the feed-specific parameters for a check run.
type FeedCheckConfig struct {
	FeedURLs []string
	Matcher  *matcher.Matcher
	RawTTL   time.Duration // TTL for raw feed items; defaults to 24h when zero.
}

// FeedCheckDeps holds the shared service dependencies for RunFeedCheck.
type FeedCheckDeps struct {
	Store      storage.Store
	Enricher   *ai.Enricher      // may be nil
	Scorer     *ai.Scorer        // may be nil
	ScorerProv ai.Provider       // may be nil
	LogBuffer  *logbuffer.Buffer // may be nil
	Logger     *zap.Logger       // may be nil; falls back to nop
	// BackfillEnabled is called each run to check whether the rescore-backfill
	// step should execute. When nil, backfill is enabled (preserves old behaviour).
	BackfillEnabled func() bool
}

// RunFeedCheck executes a full feed-check cycle: parse all feeds, match items,
// score with AI when available, stage new torrents, and backfill scores for
// previously-unscored items. Job lifecycle and SSE fan-out are handled
// internally.
func RunFeedCheck(ctx context.Context, cfg FeedCheckConfig, deps FeedCheckDeps) (models.JobSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.RawTTL == 0 {
		cfg.RawTTL = 24 * time.Hour
	}

	jobID, jobErr := deps.Store.CreateJob("feed_check")
	startedAt := time.Now()
	if jobErr != nil {
		log.Warn("could not create feed_check job", zap.Error(jobErr))
	}
	if deps.LogBuffer != nil && jobErr == nil {
		deps.LogBuffer.EmitJobEvent(models.JobRecord{
			ID:        jobID,
			Type:      "feed_check",
			Status:    "running",
			StartedAt: startedAt,
		})
	}

	parser := feed.NewParser()
	if deps.Enricher != nil {
		parser = parser.WithEnricher(deps.Enricher)
	}

	var (
		totalFound   int
		totalMatched int
		totalScored  int
		feedFailed   bool
	)
	now := time.Now()

	for _, feedURL := range cfg.FeedURLs {
		if ctx.Err() != nil {
			break
		}
		log.Info("fetching feed", zap.String("url", feedURL))

		items, err := parser.Parse(feedURL)
		if err != nil {
			log.Error("failed to parse feed", zap.String("url", feedURL), zap.Error(err))
			feedFailed = true
			continue
		}

		for _, item := range items {
			raw := models.RawFeedItem{
				FeedItem:  item,
				PulledAt:  now,
				ExpiresAt: now.Add(cfg.RawTTL),
			}
			if err := deps.Store.AddRawFeedItem(raw); err != nil {
				log.Warn("failed to store raw feed item", zap.Error(err))
			} else {
				totalFound++
			}
		}

		matches := cfg.Matcher.MatchAll(items)
		log.Info("matched items", zap.String("url", feedURL), zap.Int("count", len(matches)))

		if deps.ScorerProv != nil && deps.ScorerProv.Available() && deps.Scorer != nil {
			history, _ := deps.Store.GetActivity(50, 0, "")
			matches = deps.Scorer.ScoreAll(matches, history)
			totalScored += len(matches)
		}

		for _, match := range matches {
			if err := deps.Store.Add(match); err != nil {
				log.Warn("failed to stage torrent", zap.String("title", match.FeedItem.Title), zap.Error(err))
			} else {
				totalMatched++
			}
		}
	}

	summary := models.JobSummary{
		ItemsFound:   totalFound,
		ItemsMatched: totalMatched,
		ItemsScored:  totalScored,
	}

	if jobErr == nil {
		completedAt := time.Now()
		finalJob := models.JobRecord{
			ID:          jobID,
			Type:        "feed_check",
			StartedAt:   startedAt,
			CompletedAt: &completedAt,
			Summary:     summary,
		}
		if feedFailed {
			finalJob.Status = "failed"
			finalJob.Summary.ErrorMessage = "one or more feeds failed to parse"
			_ = deps.Store.FailJob(jobID, "one or more feeds failed to parse")
		} else {
			finalJob.Status = "completed"
			_ = deps.Store.CompleteJob(jobID, summary)
		}
		if deps.LogBuffer != nil {
			deps.LogBuffer.EmitJobEvent(finalJob)
		}
	}

	// Backfill AI scores for any torrents staged before the provider was
	// available (ai_scored=false). Covers all statuses.
	backfillOn := deps.BackfillEnabled == nil || deps.BackfillEnabled()
	if backfillOn && deps.ScorerProv != nil && deps.ScorerProv.Available() && deps.Scorer != nil {
		backfillJobID, backfillJobErr := deps.Store.CreateJob("rescore_backfill")
		if backfillJobErr != nil {
			log.Warn("could not create rescore_backfill job", zap.Error(backfillJobErr))
		}

		all, err := deps.Store.List("")
		if err == nil {
			history, _ := deps.Store.GetActivity(50, 0, "")
			backfilled := 0
			for _, t := range all {
				if t.AIScored {
					continue
				}
				scored := deps.Scorer.ScoreAll([]models.StagedTorrent{t}, history)
				if len(scored) > 0 {
					if err := deps.Store.UpdateAIScore(t.ID, scored[0].AIScore, scored[0].AIReason, scored[0].MatchConfidence, scored[0].MatchConfidenceReason); err == nil {
						backfilled++
					}
				}
			}
			summary.ItemsScored += backfilled
			log.Info("rescore backfill complete", zap.Int("backfilled", backfilled))
			if backfillJobErr == nil {
				_ = deps.Store.CompleteJob(backfillJobID, models.JobSummary{ItemsScored: backfilled})
			}
		} else if backfillJobErr == nil {
			_ = deps.Store.FailJob(backfillJobID, err.Error())
		}
	}

	var retErr error
	if feedFailed {
		retErr = fmt.Errorf("one or more feeds failed to parse")
	}
	return summary, retErr
}
