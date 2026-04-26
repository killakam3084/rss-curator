package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	Feeds   []models.FeedConfig
	Matcher *matcher.Matcher
	RawTTL  time.Duration // TTL for raw feed items; defaults to 24h when zero.
	// JobID, when non-zero, indicates the caller has already created the job
	// record and emitted the initial "running" SSE event. RunFeedCheck will
	// use this ID rather than allocating a new one.
	JobID int
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
func RunFeedCheck(ctx context.Context, cfg FeedCheckConfig, deps FeedCheckDeps) (models.FeedCheckSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.RawTTL == 0 {
		cfg.RawTTL = 24 * time.Hour
	}

	var jobID int
	var jobErr error
	startedAt := time.Now()
	if cfg.JobID > 0 {
		// Caller pre-allocated the job record and already emitted the initial event.
		jobID = cfg.JobID
	} else {
		jobID, jobErr = deps.Store.CreateJob("feed_check")
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
		allMatches   []models.StagedTorrent
	)
	now := time.Now()

	// Fetch and match all feeds in parallel; expensive network I/O and RSS
	// parsing run concurrently while SQLite writes remain serial below.
	type feedResult struct {
		rawItems []models.RawFeedItem
		matches  []models.StagedTorrent
		failed   bool
	}
	var (
		feedResultsMu sync.Mutex
		feedResults   []feedResult
		feedWg        sync.WaitGroup
	)
	for _, fc := range cfg.Feeds {
		if ctx.Err() != nil {
			break
		}
		fc := fc
		feedWg.Add(1)
		go func() {
			defer feedWg.Done()
			res := feedResult{}
			log.Info("fetching feed", zap.String("url", fc.URL), zap.String("type", string(fc.ContentType)))
			items, err := parser.Parse(fc.URL, fc.ContentType)
			if err != nil {
				log.Error("failed to parse feed", zap.String("url", fc.URL), zap.Error(err))
				res.failed = true
				feedResultsMu.Lock()
				feedResults = append(feedResults, res)
				feedResultsMu.Unlock()
				return
			}
			for _, item := range items {
				res.rawItems = append(res.rawItems, models.RawFeedItem{
					FeedItem:  item,
					PulledAt:  now,
					ExpiresAt: now.Add(cfg.RawTTL),
				})
			}
			res.matches = cfg.Matcher.MatchAll(items)
			log.Info("matched items", zap.String("url", fc.URL), zap.Int("count", len(res.matches)))
			feedResultsMu.Lock()
			feedResults = append(feedResults, res)
			feedResultsMu.Unlock()
		}()
	}
	feedWg.Wait()

	// Collect feed results serially: write raw items to storage and merge matches.
	for _, res := range feedResults {
		if res.failed {
			feedFailed = true
			continue
		}
		for _, raw := range res.rawItems {
			if err := deps.Store.AddRawFeedItem(raw); err != nil {
				log.Warn("failed to store raw feed item", zap.Error(err))
			} else {
				totalFound++
			}
		}
		allMatches = append(allMatches, res.matches...)
	}

	// Deduplicate across all feeds: for the same show+season+episode keep the
	// single best variant (by quality tier, then codec/group preference).
	allMatches = deduplicateByEpisode(allMatches)

	// Score the deduplicated match set in one concurrent batch.
	if deps.ScorerProv != nil && deps.ScorerProv.Available() && deps.Scorer != nil {
		history, _ := deps.Store.GetActivity(50, 0, "")
		allMatches = deps.Scorer.ScoreAll(allMatches, history)
		totalScored = len(allMatches)
	}
	log.Info("staging after dedup", zap.Int("count", len(allMatches)))

	for _, match := range allMatches {
		if err := deps.Store.Add(match); err != nil {
			log.Warn("failed to stage torrent", zap.String("title", match.FeedItem.Title), zap.Error(err))
		} else {
			totalMatched++
			log.Debug("staged torrent",
				zap.String("title", match.FeedItem.Title),
				zap.String("show", match.FeedItem.ShowName),
				zap.String("quality", match.FeedItem.Quality),
				zap.String("reason", match.MatchReason),
			)
		}
	}

	summary := models.FeedCheckSummary{
		ItemsFound:   totalFound,
		ItemsMatched: totalMatched,
		ItemsScored:  totalScored,
	}

	if jobErr == nil {
		completedAt := time.Now()
		summaryJSON, _ := json.Marshal(summary)
		finalJob := models.JobRecord{
			ID:          jobID,
			Type:        "feed_check",
			StartedAt:   startedAt,
			CompletedAt: &completedAt,
			Summary:     summaryJSON,
		}
		if ctx.Err() != nil {
			// Context was cancelled — job interrupted; record partial work as cancelled.
			finalJob.Status = "cancelled"
			summary.ErrorMessage = "context cancelled"
			_ = deps.Store.CancelJob(jobID, summary)
		} else if feedFailed {
			finalJob.Status = "failed"
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

		all, err := deps.Store.List("", "", "")
		if err == nil {
			history, _ := deps.Store.GetActivity(50, 0, "")
			backfilled := 0
			var unscored []models.StagedTorrent
			for _, t := range all {
				if !t.AIScored {
					unscored = append(unscored, t)
				}
			}
			if len(unscored) > 0 {
				scored := deps.Scorer.ScoreAll(unscored, history)
				for _, s := range scored {
					if err := deps.Store.UpdateAIScore(s.ID, s.AIScore, s.AIReason, s.MatchConfidence, s.MatchConfidenceReason); err == nil {
						backfilled++
					}
				}
			}
			log.Info("rescore backfill complete", zap.Int("backfilled", backfilled))
			if backfillJobErr == nil {
				_ = deps.Store.CompleteJob(backfillJobID, models.RescoreBackfillSummary{ItemsScored: backfilled})
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

// deduplicateByEpisode keeps the single best match per (show, season, episode)
// when multiple variants of the same episode are staged in one feed-check run
// (common when a broad category feed delivers many codec/quality variants at
// once). Items without a season+episode are passed through unchanged.
//
// "Best" is ranked by: quality tier (2160p > 1080p > 720p) × 4, +2 if the
// match reason signals a preferred codec, +1 for a preferred release group.
func deduplicateByEpisode(matches []models.StagedTorrent) []models.StagedTorrent {
	qualityRank := map[string]int{"720P": 1, "1080P": 2, "2160P": 3, "4K": 3}

	rank := func(t models.StagedTorrent) int {
		r := qualityRank[strings.ToUpper(t.FeedItem.Quality)] * 4
		if strings.Contains(t.MatchReason, "preferred codec") {
			r += 2
		}
		if strings.Contains(t.MatchReason, "preferred group") {
			r++
		}
		return r
	}

	type episodeKey struct {
		show    string
		season  int
		episode int
	}

	best := make(map[episodeKey]models.StagedTorrent)
	var unkeyed []models.StagedTorrent

	for _, m := range matches {
		fi := m.FeedItem
		if fi.Season == 0 && fi.Episode == 0 {
			// Season pack or unrecognised pattern — pass through.
			unkeyed = append(unkeyed, m)
			continue
		}
		k := episodeKey{
			show:    strings.ToLower(strings.TrimSpace(fi.ShowName)),
			season:  fi.Season,
			episode: fi.Episode,
		}
		if existing, ok := best[k]; !ok || rank(m) > rank(existing) {
			best[k] = m
		}
	}

	result := make([]models.StagedTorrent, 0, len(best)+len(unkeyed))
	for _, t := range best {
		result = append(result, t)
	}
	return append(result, unkeyed...)
}
