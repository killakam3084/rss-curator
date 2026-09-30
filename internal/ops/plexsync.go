package ops

import (
	"context"
	"fmt"
	"time"

	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/plex"
	"github.com/killakam3084/rss-curator/internal/storage"
	"go.uber.org/zap"
)

// plexUpsertBatch bounds how many rows are buffered before a write.
const plexUpsertBatch = 200

// PlexStore is the storage surface the Plex operations need. It is declared
// here rather than added to storage.Store so the broader interface (and its
// test doubles) stay untouched.
type PlexStore interface {
	PlexKnownExternalIDs(sectionKey string) (map[string]storage.ExternalIDs, error)
	UpsertPlexShows(shows []storage.PlexShow) error
	UpsertPlexItems(items []storage.PlexItem) error
	UpdatePlexItemMedia(ratingKey, resolution, codec string, hdr []string, fileSize int64) error
	DeleteStalePlexRows(sectionKey string, before time.Time) (int64, error)
	UpsertPlexLibrary(lib storage.PlexLibrary) error
	GetPlexCounts() (storage.PlexCounts, error)
}

// PlexSyncConfig selects what to sync.
type PlexSyncConfig struct {
	// SectionKeys limits the sync to specific library sections. Empty means
	// every movie and show section on the server.
	SectionKeys []string
}

// PlexSyncDeps holds the shared service dependencies for RunPlexSync.
type PlexSyncDeps struct {
	Store  PlexStore
	Plex   *client.Plex // nil when Plex is disabled or unreachable
	Logger *zap.Logger  // may be nil
}

// PlexSyncSummary is the result returned by RunPlexSync.
type PlexSyncSummary struct {
	Sections   int      `json:"sections"`
	Shows      int      `json:"shows"`
	Episodes   int      `json:"episodes"`
	Movies     int      `json:"movies"`
	Removed    int64    `json:"removed"`
	GUIDLookup int      `json:"guid_lookups"`
	Errors     []string `json:"errors,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// RunPlexSync refreshes the cached copy of the Plex library.
//
// Shows are synced before episodes because show-level identifiers are the
// join key: Plex's episode GUIDs are not dependable, so each episode is linked
// to its show via grandparentRatingKey instead.
func RunPlexSync(ctx context.Context, cfg PlexSyncConfig, deps PlexSyncDeps) (PlexSyncSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	started := time.Now()
	summary := PlexSyncSummary{}

	if deps.Plex == nil {
		return summary, fmt.Errorf("plex sync: client unavailable")
	}

	sections, err := deps.Plex.Sections(ctx)
	if err != nil {
		return summary, fmt.Errorf("plex sync: list sections: %w", err)
	}

	wanted := map[string]bool{}
	for _, key := range cfg.SectionKeys {
		wanted[key] = true
	}

	syncedAt := time.Now().UTC()
	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if len(wanted) > 0 && !wanted[section.Key] {
			continue
		}
		if section.Type != "show" && section.Type != "movie" {
			continue
		}

		count, err := syncSection(ctx, section, syncedAt, deps, &summary)
		if err != nil {
			log.Warn("plex section sync failed",
				zap.String("section", section.Title), zap.Error(err))
			summary.Errors = append(summary.Errors,
				fmt.Sprintf("%s: %v", section.Title, err))
			continue
		}
		summary.Sections++

		removed, err := deps.Store.DeleteStalePlexRows(section.Key, syncedAt)
		if err != nil {
			log.Warn("plex stale sweep failed",
				zap.String("section", section.Title), zap.Error(err))
		}
		summary.Removed += removed

		if err := deps.Store.UpsertPlexLibrary(storage.PlexLibrary{
			SectionKey:   section.Key,
			Title:        section.Title,
			Type:         section.Type,
			ItemCount:    count,
			LastSyncedAt: syncedAt,
		}); err != nil {
			log.Warn("plex library record failed",
				zap.String("section", section.Title), zap.Error(err))
		}
	}

	summary.DurationMS = time.Since(started).Milliseconds()
	log.Info("plex sync complete",
		zap.Int("sections", summary.Sections),
		zap.Int("shows", summary.Shows),
		zap.Int("episodes", summary.Episodes),
		zap.Int("movies", summary.Movies),
		zap.Int64("removed", summary.Removed),
		zap.Int("guid_lookups", summary.GUIDLookup))
	return summary, nil
}

func syncSection(
	ctx context.Context,
	section client.PlexSection,
	syncedAt time.Time,
	deps PlexSyncDeps,
	summary *PlexSyncSummary,
) (int, error) {
	known, err := deps.Store.PlexKnownExternalIDs(section.Key)
	if err != nil {
		return 0, err
	}

	if section.Type == "movie" {
		return syncMovies(ctx, section, syncedAt, known, deps, summary)
	}
	return syncShows(ctx, section, syncedAt, known, deps, summary)
}

func syncShows(
	ctx context.Context,
	section client.PlexSection,
	syncedAt time.Time,
	known map[string]storage.ExternalIDs,
	deps PlexSyncDeps,
	summary *PlexSyncSummary,
) (int, error) {
	var batch []storage.PlexShow
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := deps.Store.UpsertPlexShows(batch); err != nil {
			return err
		}
		summary.Shows += len(batch)
		batch = batch[:0]
		return nil
	}

	err := deps.Plex.Shows(ctx, section.Key, func(page []client.PlexItem) error {
		for _, item := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			ids := resolveExternalIDs(ctx, item, known, deps, summary)
			batch = append(batch, storage.PlexShow{
				RatingKey:    item.RatingKey,
				SectionKey:   section.Key,
				Title:        item.Title,
				NormTitle:    plex.NormalizeTitle(item.Title),
				NormAltTitle: plex.NormalizeTitle(item.OriginalTitle),
				Year:         item.Year,
				IMDbID:       ids.IMDbID,
				TMDBID:       ids.TMDBID,
				TVDBID:       ids.TVDBID,
				SyncedAt:     syncedAt,
			})
			if len(batch) >= plexUpsertBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sync shows: %w", err)
	}
	if err := flush(); err != nil {
		return 0, err
	}

	episodes, err := syncEpisodes(ctx, section, syncedAt, deps, summary)
	if err != nil {
		return 0, err
	}
	return episodes, nil
}

func syncEpisodes(
	ctx context.Context,
	section client.PlexSection,
	syncedAt time.Time,
	deps PlexSyncDeps,
	summary *PlexSyncSummary,
) (int, error) {
	var batch []storage.PlexItem
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := deps.Store.UpsertPlexItems(batch); err != nil {
			return err
		}
		summary.Episodes += len(batch)
		total += len(batch)
		batch = batch[:0]
		return nil
	}

	err := deps.Plex.Episodes(ctx, section.Key, func(page []client.PlexItem) error {
		for _, item := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Episode identity comes from the owning show plus season and
			// episode numbers, so no per-episode id lookup is needed.
			batch = append(batch, storage.PlexItem{
				RatingKey:     item.RatingKey,
				SectionKey:    section.Key,
				ContentType:   "episode",
				ShowRatingKey: item.GrandparentRatingKey,
				Title:         item.Title,
				NormTitle:     plex.NormalizeTitle(item.Title),
				Season:        item.Season,
				Episode:       item.Episode,
				Resolution:    item.Resolution,
				Codec:         item.Codec,
				HDR:           item.HDR,
				FileSize:      item.FileSize,
				FilePath:      item.FilePath,
				SyncedAt:      syncedAt,
			})
			if len(batch) >= plexUpsertBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return total, fmt.Errorf("sync episodes: %w", err)
	}
	return total, flush()
}

func syncMovies(
	ctx context.Context,
	section client.PlexSection,
	syncedAt time.Time,
	known map[string]storage.ExternalIDs,
	deps PlexSyncDeps,
	summary *PlexSyncSummary,
) (int, error) {
	var batch []storage.PlexItem
	total := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := deps.Store.UpsertPlexItems(batch); err != nil {
			return err
		}
		summary.Movies += len(batch)
		total += len(batch)
		batch = batch[:0]
		return nil
	}

	err := deps.Plex.Movies(ctx, section.Key, func(page []client.PlexItem) error {
		for _, item := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			ids := resolveExternalIDs(ctx, item, known, deps, summary)
			batch = append(batch, storage.PlexItem{
				RatingKey:    item.RatingKey,
				SectionKey:   section.Key,
				ContentType:  "movie",
				Title:        item.Title,
				NormTitle:    plex.NormalizeTitle(item.Title),
				NormAltTitle: plex.NormalizeTitle(item.OriginalTitle),
				Year:         item.Year,
				IMDbID:       ids.IMDbID,
				TMDBID:       ids.TMDBID,
				TVDBID:       ids.TVDBID,
				Resolution:   item.Resolution,
				Codec:        item.Codec,
				HDR:          item.HDR,
				FileSize:     item.FileSize,
				FilePath:     item.FilePath,
				SyncedAt:     syncedAt,
			})
			if len(batch) >= plexUpsertBatch {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return total, fmt.Errorf("sync movies: %w", err)
	}
	return total, flush()
}

// resolveExternalIDs finds the cross-provider ids for a library entry.
//
// Section listings do not return them, so they come from the cache when a
// previous run already resolved them, and otherwise from a one-off detail
// request. In steady state only newly added titles cost a request.
func resolveExternalIDs(
	ctx context.Context,
	item client.PlexItem,
	known map[string]storage.ExternalIDs,
	deps PlexSyncDeps,
	summary *PlexSyncSummary,
) storage.ExternalIDs {
	if item.HasExternalIDs() {
		return storage.ExternalIDs{IMDbID: item.IMDbID, TMDBID: item.TMDBID, TVDBID: item.TVDBID}
	}
	if cached, ok := known[item.RatingKey]; ok && cached.Any() {
		return cached
	}

	detail, err := deps.Plex.Item(ctx, item.RatingKey)
	summary.GUIDLookup++
	if err != nil || detail == nil {
		return storage.ExternalIDs{}
	}
	return storage.ExternalIDs{IMDbID: detail.IMDbID, TMDBID: detail.TMDBID, TVDBID: detail.TVDBID}
}
