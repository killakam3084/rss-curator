package ops

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// AutoQueueConfig controls selection thresholds and dry-run mode.
type AutoQueueConfig struct {
	// MinAIScore is the minimum AIScore [0.0–1.0] a candidate must have.
	MinAIScore float64
	// MinConfidence is the minimum MatchConfidence [0.0–1.0] a candidate must have.
	MinConfidence float64
	// DryRun when true runs selection logic without writing to the store or
	// qBittorrent. All decisions are still recorded in the summary.
	DryRun bool
}

// AutoQueueDeps holds shared service dependencies.
type AutoQueueDeps struct {
	Store   storage.Store
	QB      *client.Client // may be nil; queuing is skipped when nil
	Matcher *matcher.Matcher
	Logger  *zap.Logger // may be nil
}

// AutoQueueDecision records what the auto-queue job decided for one episode
// group. It is populated in both live and dry-run modes.
type AutoQueueDecision struct {
	ShowName       string                `json:"show_name"`
	Episode        string                `json:"episode"`
	Winner         *models.StagedTorrent `json:"winner,omitempty"`
	Score          float64               `json:"score"`
	ScoreBreakdown string                `json:"score_breakdown"`
	SkipReason     string                `json:"skip_reason,omitempty"`
	Skipped        bool                  `json:"skipped"`
	DryRun         bool                  `json:"dry_run"`
	Err            string                `json:"error,omitempty"`
}

// AutoQueueSummary is the result returned by RunAutoQueue.
type AutoQueueSummary struct {
	Evaluated  int                 `json:"evaluated"`  // episode groups assessed
	Queued     int                 `json:"queued"`     // winners sent to qBittorrent
	Skipped    int                 `json:"skipped"`    // groups skipped (threshold / per-show opt-out)
	Failed     int                 `json:"failed"`     // winners that failed to add to qBittorrent
	Selections []AutoQueueDecision `json:"selections"` // per-group decision log
}

// episodeKey groups candidates by (show_name_lower, season, episode).
type episodeKey struct {
	show    string
	season  int
	episode int
}

// qualityRankAQ maps quality tier strings to integer ranks for scoring.
var qualityRankAQ = map[string]int{
	"480P":  0,
	"720P":  2,
	"1080P": 5,
	"2160P": 8,
	"4K":    8,
}

// fileSizeBaselines maps quality tier to expected bytes (approximate medians).
var fileSizeBaselines = map[string]int64{
	"720P":  int64(1.5 * 1024 * 1024 * 1024),  // ~1.5 GB
	"1080P": int64(8.0 * 1024 * 1024 * 1024),  // ~8 GB
	"2160P": int64(20.0 * 1024 * 1024 * 1024), // ~20 GB
	"4K":    int64(20.0 * 1024 * 1024 * 1024), // ~20 GB
}

// candidateScore computes an ephemeral composite score [0–100] for a staged
// torrent. It is never persisted — used only to break ties among candidates
// that all pass the AI score / confidence thresholds.
//
// Weights:
//
//	AIScore × 60          = 0–60
//	GroupReputation × 20  = 0–20
//	QualityTier           = 0–8  (480P=0, 720P=2, 1080P=5, 2160P/4K=8)
//	CodecPreference       = 0–4  (preferred=4, present/no-pref=2, other=0)
//	HDRPreference         = 0–4  (any preferred tag matched=4, no preference=0 neutral)
//	FileSizeSignal        = 0–2  (within ±50% baseline=2, otherwise 0)
//	RecencyBonus          = 0–2  (decay from 24h, zero at 30h+)
//	RARPenalty            = -3   (.rar / RAR in title)
func candidateScore(
	t models.StagedTorrent,
	groupStats map[string]float64,
	showRule *models.ShowRule,
	movieRule *models.MovieRule,
	defaultRules models.DefaultRules,
	now time.Time,
) (score float64, breakdown string) {
	var parts []string

	// — AI score base (0–60) —
	aiPts := t.AIScore * 60
	score += aiPts
	parts = append(parts, fmt.Sprintf("ai=%.1f", aiPts))

	// — Group reputation (0–20) —
	grpPts := 0.0
	if len(groupStats) > 0 && t.FeedItem.ReleaseGroup != "" {
		grpPts = groupStats[t.FeedItem.ReleaseGroup] * 20
	}
	score += grpPts
	parts = append(parts, fmt.Sprintf("grp=%.1f", grpPts))

	// — Quality tier (0–8) —
	qualPts := float64(qualityRankAQ[strings.ToUpper(t.FeedItem.Quality)])
	score += qualPts
	parts = append(parts, fmt.Sprintf("qual=%.0f", qualPts))

	// — Codec preference (0–4) —
	preferredCodec := defaultRules.PreferredCodec
	if showRule != nil && showRule.PreferredCodec != "" {
		preferredCodec = showRule.PreferredCodec
	} else if movieRule != nil && movieRule.PreferredCodec != "" {
		preferredCodec = movieRule.PreferredCodec
	}
	codecPts := 0.0
	if t.FeedItem.Codec != "" {
		if preferredCodec != "" && strings.EqualFold(t.FeedItem.Codec, preferredCodec) {
			codecPts = 4
		} else if preferredCodec == "" {
			codecPts = 2 // codec present, no preference set
		}
	}
	score += codecPts
	parts = append(parts, fmt.Sprintf("codec=%.0f", codecPts))

	// — HDR preference (0–4) —
	var preferredHDR []string
	if showRule != nil && len(showRule.PreferredHDR) > 0 {
		preferredHDR = showRule.PreferredHDR
	} else if movieRule != nil && len(movieRule.PreferredHDR) > 0 {
		preferredHDR = movieRule.PreferredHDR
	} else {
		preferredHDR = defaultRules.PreferredHDR
	}
	hdrPts := 0.0
	if len(preferredHDR) > 0 && isPreferredHDRAQ(t.FeedItem.HDR, preferredHDR) {
		hdrPts = 4
	}
	score += hdrPts
	parts = append(parts, fmt.Sprintf("hdr=%.0f", hdrPts))

	// — File size signal (0–2) —
	sizePts := 0.0
	if t.FeedItem.Size > 0 {
		baseline, ok := fileSizeBaselines[strings.ToUpper(t.FeedItem.Quality)]
		if ok && baseline > 0 {
			ratio := float64(t.FeedItem.Size) / float64(baseline)
			if ratio >= 0.5 && ratio <= 1.5 {
				sizePts = 2
			}
		}
	}
	score += sizePts
	parts = append(parts, fmt.Sprintf("size=%.0f", sizePts))

	// — Recency bonus (0–2): decay from 0h to 30h —
	recencyPts := 0.0
	if !t.FeedItem.PubDate.IsZero() {
		hoursAgo := now.Sub(t.FeedItem.PubDate).Hours()
		if hoursAgo < 30 {
			recencyPts = math.Max(0, 2.0*(1.0-hoursAgo/30.0))
		}
	}
	score += recencyPts
	parts = append(parts, fmt.Sprintf("recency=%.1f", recencyPts))

	// — RAR penalty (-3) —
	titleLower := strings.ToLower(t.FeedItem.Title)
	if strings.Contains(titleLower, ".rar") || strings.Contains(titleLower, " rar") {
		score -= 3
		parts = append(parts, "rar=-3")
	}

	return score, strings.Join(parts, " ")
}

// isPreferredHDRAQ is a local copy of the matcher's isPreferredHDR function to
// avoid an import cycle between ops and matcher.
func isPreferredHDRAQ(itemHDR []string, preferred []string) bool {
	for _, h := range itemHDR {
		for _, p := range preferred {
			if strings.EqualFold(h, p) {
				return true
			}
		}
	}
	return false
}

// autoQueueEnabled reports whether auto-queue is enabled for the matched rule.
// showRule and movieRule are looked up from the watchlist; a nil pointer means
// no per-item rule was found. The global enabled flag is passed as globalEnabled.
func autoQueueEnabled(showRule *models.ShowRule, movieRule *models.MovieRule, globalEnabled bool) bool {
	if showRule != nil && showRule.AutoQueue != nil {
		return *showRule.AutoQueue
	}
	if movieRule != nil && movieRule.AutoQueue != nil {
		return *movieRule.AutoQueue
	}
	return globalEnabled
}

// lookupRule returns the ShowRule or MovieRule for the given torrent by
// searching the watchlist. Returns nil for both if no matching rule is found.
func lookupRule(cfg *models.ShowsConfig, t models.StagedTorrent) (*models.ShowRule, *models.MovieRule) {
	if cfg == nil {
		return nil, nil
	}
	name := strings.TrimSpace(t.FeedItem.ShowName)
	if t.FeedItem.ContentType == models.ContentTypeMovie {
		for i := range cfg.Movies {
			if strings.EqualFold(cfg.Movies[i].Name, name) {
				return nil, &cfg.Movies[i]
			}
		}
		return nil, nil
	}
	for i := range cfg.Shows {
		if strings.EqualFold(cfg.Shows[i].Name, name) {
			return &cfg.Shows[i], nil
		}
	}
	return nil, nil
}

// RunAutoQueue executes one auto-queue cycle: for each pending episode group
// that has at least one AI-scored candidate meeting the configured thresholds,
// it selects the highest composite-scored candidate and queues it to
// qBittorrent. Items without a season+episode number are skipped (season packs
// and movies without episode metadata are out of scope for this first pass).
//
// Losing candidates within a selected group remain in 'pending' status for
// human review. Failed additions are marked 'failed' in the store.
//
// When cfg.DryRun is true, no writes are made to the store or qBittorrent but
// all selection decisions are still recorded in the returned summary.
func RunAutoQueue(ctx context.Context, cfg AutoQueueConfig, deps AutoQueueDeps) (AutoQueueSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}

	var summary AutoQueueSummary

	// Fetch all pending torrents.
	pending, err := deps.Store.List("pending", "", "")
	if err != nil {
		return summary, fmt.Errorf("auto_queue: list pending: %w", err)
	}
	if len(pending) == 0 {
		log.Info("auto_queue: no pending torrents")
		return summary, nil
	}

	// Load group reputation stats.
	groupStats, err := deps.Store.GetGroupReputationStats()
	if err != nil {
		log.Warn("auto_queue: could not load group stats; proceeding without", zap.Error(err))
		groupStats = map[string]float64{}
	}

	// Load watchlist config for per-show AutoQueue and rule lookups.
	watchlistCfg := deps.Matcher.ShowsConfig()

	now := time.Now()

	// Group by (show_lower, season, episode). Skip items without episode info.
	groups := make(map[episodeKey][]models.StagedTorrent)
	for _, t := range pending {
		if t.FeedItem.Season == 0 && t.FeedItem.Episode == 0 {
			continue // season pack / unrecognised — skip
		}
		k := episodeKey{
			show:    strings.ToLower(strings.TrimSpace(t.FeedItem.ShowName)),
			season:  t.FeedItem.Season,
			episode: t.FeedItem.Episode,
		}
		groups[k] = append(groups[k], t)
	}

	for _, candidates := range groups {
		if ctx.Err() != nil {
			break
		}
		summary.Evaluated++

		// Representative item for display.
		rep := candidates[0]
		epLabel := fmt.Sprintf("S%02dE%02d", rep.FeedItem.Season, rep.FeedItem.Episode)

		// Filter to candidates that pass thresholds.
		var eligible []models.StagedTorrent
		for _, c := range candidates {
			if !c.AIScored {
				continue
			}
			if c.AIScore < cfg.MinAIScore {
				continue
			}
			if c.MatchConfidence >= 0 && c.MatchConfidence < cfg.MinConfidence {
				continue
			}
			eligible = append(eligible, c)
		}
		if len(eligible) == 0 {
			summary.Skipped++
			summary.Selections = append(summary.Selections, AutoQueueDecision{
				ShowName:   rep.FeedItem.ShowName,
				Episode:    epLabel,
				Skipped:    true,
				SkipReason: "no candidate meets score/confidence thresholds",
			})
			continue
		}

		// Look up rule for the first eligible candidate (all same show).
		showRule, movieRule := lookupRule(watchlistCfg, eligible[0])

		// Check per-show auto-queue opt-out.
		if !autoQueueEnabled(showRule, movieRule, true /* caller gates on global enabled */) {
			summary.Skipped++
			summary.Selections = append(summary.Selections, AutoQueueDecision{
				ShowName:   rep.FeedItem.ShowName,
				Episode:    epLabel,
				Skipped:    true,
				SkipReason: "auto_queue disabled for this show/movie in watchlist",
			})
			continue
		}

		// Score and pick winner.
		defaultRules := models.DefaultRules{}
		if watchlistCfg != nil {
			defaultRules = watchlistCfg.Defaults
		}
		bestScore := -math.MaxFloat64
		var winner models.StagedTorrent
		var bestBreakdown string
		for _, c := range eligible {
			s, breakdown := candidateScore(c, groupStats, showRule, movieRule, defaultRules, now)
			if s > bestScore {
				bestScore = s
				winner = c
				bestBreakdown = breakdown
			}
		}

		decision := AutoQueueDecision{
			ShowName:       rep.FeedItem.ShowName,
			Episode:        epLabel,
			Winner:         &winner,
			Score:          bestScore,
			ScoreBreakdown: bestBreakdown,
			DryRun:         cfg.DryRun,
		}

		if cfg.DryRun {
			summary.Queued++ // count as "would queue"
			summary.Selections = append(summary.Selections, decision)
			log.Info("auto_queue: dry-run would queue",
				zap.String("show", rep.FeedItem.ShowName),
				zap.String("episode", epLabel),
				zap.String("title", winner.FeedItem.Title),
				zap.Float64("composite_score", bestScore),
				zap.String("breakdown", bestBreakdown),
			)
			continue
		}

		if deps.QB == nil {
			summary.Skipped++
			decision.Skipped = true
			decision.SkipReason = "qBittorrent client unavailable"
			summary.Selections = append(summary.Selections, decision)
			continue
		}

		// Transition: pending → accepted → queued.
		if err := deps.Store.UpdateStatus(winner.ID, "accepted"); err != nil {
			log.Error("auto_queue: UpdateStatus accepted failed",
				zap.Int("id", winner.ID), zap.Error(err))
			summary.Failed++
			decision.Err = err.Error()
			summary.Selections = append(summary.Selections, decision)
			continue
		}

		if err := deps.QB.AddTorrent(winner.FeedItem.Link, nil); err != nil {
			log.Error("auto_queue: AddTorrent failed",
				zap.String("title", winner.FeedItem.Title), zap.Error(err))
			_ = deps.Store.SetFailed(winner.ID, err.Error())
			_ = deps.Store.LogActivity(winner.ID, winner.FeedItem.Title, "auto_queue_failed", winner.MatchReason)
			summary.Failed++
			decision.Err = err.Error()
			summary.Selections = append(summary.Selections, decision)
			continue
		}

		if err := deps.Store.UpdateStatus(winner.ID, "queued"); err != nil {
			log.Warn("auto_queue: UpdateStatus queued failed",
				zap.Int("id", winner.ID), zap.Error(err))
		}
		_ = deps.Store.LogActivity(winner.ID, winner.FeedItem.Title, "auto_queue", winner.MatchReason)

		summary.Queued++
		summary.Selections = append(summary.Selections, decision)
		log.Info("auto_queue: queued",
			zap.String("show", rep.FeedItem.ShowName),
			zap.String("episode", epLabel),
			zap.String("title", winner.FeedItem.Title),
			zap.Float64("composite_score", bestScore),
			zap.String("breakdown", bestBreakdown),
		)
	}

	return summary, nil
}

// AutoQueueSummaryFromStore is a thin helper used by the job runner to build a
// storage-compatible summary value. It satisfies the any constraint accepted by
// storage.CompleteJob.
func autoQueueSummaryAny(s AutoQueueSummary) any { return s }

// RunAutoQueueJob wraps RunAutoQueue with job lifecycle management (create job
// record, complete/fail). Intended to be called from the scheduler or job queue.
func RunAutoQueueJob(ctx context.Context, cfg AutoQueueConfig, deps AutoQueueDeps) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}

	jobID, err := deps.Store.CreateJob("auto_queue")
	if err != nil {
		log.Warn("auto_queue: could not create job record", zap.Error(err))
	}

	summary, runErr := RunAutoQueue(ctx, cfg, deps)

	if jobID > 0 {
		if runErr != nil {
			_ = deps.Store.FailJob(jobID, runErr.Error())
		} else {
			_ = deps.Store.CompleteJob(jobID, autoQueueSummaryAny(summary))
		}
	}

	if runErr != nil {
		log.Error("auto_queue: run failed", zap.Error(runErr))
	} else {
		log.Info("auto_queue: completed",
			zap.Int("evaluated", summary.Evaluated),
			zap.Int("queued", summary.Queued),
			zap.Int("skipped", summary.Skipped),
			zap.Int("failed", summary.Failed),
		)
	}
}
