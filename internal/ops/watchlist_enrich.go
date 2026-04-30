package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/killakam3084/rss-curator/internal/feed"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// WatchlistEnrichDeps holds the shared dependencies for RunWatchlistEnrich.
type WatchlistEnrichDeps struct {
	Store     storage.Store
	Matcher   *matcher.Matcher
	ShowsPath string
	Logger    *zap.Logger
}

// RunWatchlistEnrich backfills empty ShowRule/MovieRule fields (preferred_codec,
// preferred_groups, preferred_hdr) from approval history. Fields that are
// already populated are never overwritten.
func RunWatchlistEnrich(ctx context.Context, deps WatchlistEnrichDeps) (models.WatchlistEnrichSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}

	jobID, jobErr := deps.Store.CreateJob("watchlist_enrich")
	if jobErr != nil {
		log.Warn("could not create watchlist_enrich job", zap.Error(jobErr))
	}

	summary, err := runWatchlistEnrich(ctx, deps, log)
	if err != nil {
		if jobErr == nil {
			_ = deps.Store.FailJob(jobID, err.Error())
		}
		return summary, err
	}
	if jobErr == nil {
		_ = deps.Store.CompleteJob(jobID, summary)
	}
	return summary, nil
}

func runWatchlistEnrich(ctx context.Context, deps WatchlistEnrichDeps, log *zap.Logger) (models.WatchlistEnrichSummary, error) {
	var summary models.WatchlistEnrichSummary

	// Fetch recent approvals — up to 500 entries is more than enough for
	// deterministic enrichment without unbounded memory usage.
	activities, err := deps.Store.GetActivity(500, 0, "approve")
	if err != nil {
		return summary, fmt.Errorf("watchlist_enrich: get activity: %w", err)
	}

	// Accumulate signal per normalized name extracted from each match reason.
	type nameData struct {
		codecCounts map[string]int
		groupsSeen  map[string]bool
		hdrSeen     map[string]bool
		contentType models.ContentType
	}
	data := make(map[string]*nameData)

	for _, a := range activities {
		name, ct := enrichNameFromMatchReason(a.MatchReason)
		if name == "" {
			continue
		}
		key := enrichNormalizeName(name)
		if _, ok := data[key]; !ok {
			data[key] = &nameData{
				codecCounts: make(map[string]int),
				groupsSeen:  make(map[string]bool),
				hdrSeen:     make(map[string]bool),
				contentType: ct,
			}
		}
		nd := data[key]

		// Re-parse the original torrent title to extract codec/group/HDR.
		item := &models.FeedItem{
			Title:       a.TorrentTitle,
			ContentType: ct,
		}
		feed.ParseTitleMetadata(item)

		if item.Codec != "" {
			nd.codecCounts[item.Codec]++
		}
		if item.ReleaseGroup != "" {
			nd.groupsSeen[strings.ToLower(item.ReleaseGroup)] = true
		}
		for _, h := range item.HDR {
			nd.hdrSeen[h] = true
		}
	}

	// Load current shows configuration for mutation.
	cfg := deps.Matcher.ShowsConfig()
	if cfg == nil {
		return summary, nil
	}

	// Fill empty ShowRule fields.
	for i, rule := range cfg.Shows {
		key := enrichNormalizeName(rule.Name)
		nd, ok := data[key]
		if !ok || nd.contentType != models.ContentTypeShow {
			continue
		}
		changed := false
		if rule.PreferredCodec == "" {
			if codec := enrichModeCodec(nd.codecCounts); codec != "" {
				cfg.Shows[i].PreferredCodec = codec
				changed = true
			}
		}
		if len(rule.PreferredGroups) == 0 {
			if groups := enrichSortedKeys(nd.groupsSeen); len(groups) > 0 {
				cfg.Shows[i].PreferredGroups = groups
				changed = true
			}
		}
		if len(rule.PreferredHDR) == 0 {
			if hdrs := enrichSortedKeys(nd.hdrSeen); len(hdrs) > 0 {
				cfg.Shows[i].PreferredHDR = hdrs
				changed = true
			}
		}
		if changed {
			summary.ShowsUpdated++
		}
	}

	// Fill empty MovieRule fields.
	for i, rule := range cfg.Movies {
		key := enrichNormalizeName(rule.Name)
		nd, ok := data[key]
		if !ok || nd.contentType != models.ContentTypeMovie {
			continue
		}
		changed := false
		if rule.PreferredCodec == "" {
			if codec := enrichModeCodec(nd.codecCounts); codec != "" {
				cfg.Movies[i].PreferredCodec = codec
				changed = true
			}
		}
		if len(rule.PreferredGroups) == 0 {
			if groups := enrichSortedKeys(nd.groupsSeen); len(groups) > 0 {
				cfg.Movies[i].PreferredGroups = groups
				changed = true
			}
		}
		if len(rule.PreferredHDR) == 0 {
			if hdrs := enrichSortedKeys(nd.hdrSeen); len(hdrs) > 0 {
				cfg.Movies[i].PreferredHDR = hdrs
				changed = true
			}
		}
		if changed {
			summary.MoviesUpdated++
		}
	}

	// Persist and hot-reload only when something actually changed.
	if summary.ShowsUpdated+summary.MoviesUpdated > 0 {
		out, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return summary, fmt.Errorf("watchlist_enrich: marshal config: %w", err)
		}
		if err := os.WriteFile(deps.ShowsPath, out, 0o644); err != nil {
			return summary, fmt.Errorf("watchlist_enrich: write shows.json: %w", err)
		}
		deps.Matcher.SetShowsConfig(cfg)
		log.Info("watchlist_enrich completed",
			zap.Int("shows_updated", summary.ShowsUpdated),
			zap.Int("movies_updated", summary.MoviesUpdated),
		)
	} else {
		log.Info("watchlist_enrich: no changes needed")
	}

	return summary, nil
}

// enrichNameFromMatchReason parses a match reason like
// "matches show: Dark, quality: 1080P" or "matches movie: Oppenheimer, ..."
// and returns the content name and its ContentType.
func enrichNameFromMatchReason(reason string) (string, models.ContentType) {
	const showPrefix = "matches show: "
	const moviePrefix = "matches movie: "
	if strings.HasPrefix(reason, showPrefix) {
		rest := strings.TrimPrefix(reason, showPrefix)
		name := strings.SplitN(rest, ",", 2)[0]
		return strings.TrimSpace(name), models.ContentTypeShow
	}
	if strings.HasPrefix(reason, moviePrefix) {
		rest := strings.TrimPrefix(reason, moviePrefix)
		name := strings.SplitN(rest, ",", 2)[0]
		return strings.TrimSpace(name), models.ContentTypeMovie
	}
	return "", ""
}

// enrichNormalizeName returns a lowercase, trimmed lookup key.
func enrichNormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// enrichModeCodec returns the codec with the highest approval count.
// Ties broken alphabetically for determinism.
func enrichModeCodec(counts map[string]int) string {
	best, bestN := "", 0
	for codec, n := range counts {
		if codec == "" {
			continue
		}
		if n > bestN || (n == bestN && codec < best) {
			best, bestN = codec, n
		}
	}
	return best
}

// enrichSortedKeys returns a sorted slice of all true keys in a map.
func enrichSortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
