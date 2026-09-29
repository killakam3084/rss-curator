package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/plex"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// Match tiers recorded on every annotation. A guid match is an exact
// cross-provider identity; a title match is a normalized-string guess and is
// surfaced differently in the UI so it can be eyeballed before bulk action.
const (
	MatchTierGUID  = "guid"
	MatchTierTitle = "title"
)

// reconcileStatuses are the staged statuses worth annotating. Rejected and
// queued torrents are already resolved.
var reconcileStatuses = []string{"pending", "accepted", "failed"}

// PlexReconcileStore is the storage surface the reconciler needs.
type PlexReconcileStore interface {
	List(status, query, contentType string) ([]models.StagedTorrent, error)
	LookupPlexShowByIDs(imdbID, tmdbID, tvdbID string) (*storage.PlexShow, error)
	LookupPlexShowByTitle(normTitle string) (*storage.PlexShow, error)
	LookupPlexEpisode(showRatingKey string, season, episode int) (*storage.PlexItem, error)
	LookupPlexMovieByIDs(imdbID, tmdbID, tvdbID string) (*storage.PlexItem, error)
	LookupPlexMovieByTitle(normTitle string, year int) (*storage.PlexItem, error)
	UpdatePlexItemMedia(ratingKey, resolution, codec string, hdr []string, fileSize int64) error
	UpsertAnnotations(annotations []storage.Annotation) error
	DeleteAnnotationsBySource(torrentIDs []int, source string) (int64, error)
}

// MetadataResolver resolves a title to its external provider ids.
// *metadata.Lookup satisfies it; nil disables the guid tier.
type MetadataResolver interface {
	Resolve(ctx context.Context, showName string) *metadata.ShowMetadata
	ResolveMovie(ctx context.Context, movieName string) *metadata.ShowMetadata
}

// PlexReconcileDeps holds the shared dependencies for RunPlexReconcile.
type PlexReconcileDeps struct {
	Store    PlexReconcileStore
	Metadata MetadataResolver // may be nil
	Plex     *client.Plex     // may be nil; only used to top up missing HDR
	Logger   *zap.Logger      // may be nil
}

// PlexAnnotationDetail is the JSON payload stored with each annotation. It
// explains the verdict so the UI can show what the library already holds and
// how confident the identity match was.
type PlexAnnotationDetail struct {
	MatchTier     string   `json:"match_tier"`
	MatchedOn     string   `json:"matched_on"`
	Reason        string   `json:"reason"`
	PlexRatingKey string   `json:"plex_rating_key"`
	PlexTitle     string   `json:"plex_title"`
	PlexQuality   string   `json:"plex_quality,omitempty"`
	PlexCodec     string   `json:"plex_codec,omitempty"`
	PlexHDR       []string `json:"plex_hdr,omitempty"`
	PlexFileSize  int64    `json:"plex_file_size,omitempty"`
}

// PlexReconcileSummary is the result returned by RunPlexReconcile.
type PlexReconcileSummary struct {
	Examined   int      `json:"examined"`
	Matched    int      `json:"matched"`
	InLibrary  int      `json:"in_library"`
	Upgrades   int      `json:"upgrades"`
	Cleared    int64    `json:"cleared"`
	GUIDTier   int      `json:"guid_tier"`
	TitleTier  int      `json:"title_tier"`
	Skipped    int      `json:"skipped"`
	Errors     []string `json:"errors,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// RunPlexReconcile annotates staged torrents against the cached Plex library.
//
// It never changes a torrent's status. Identity is resolved by external
// provider id where possible and by normalized title otherwise, and the tier
// used is recorded on the annotation.
func RunPlexReconcile(ctx context.Context, deps PlexReconcileDeps) (PlexReconcileSummary, error) {
	log := deps.Logger
	if log == nil {
		log = zap.NewNop()
	}
	started := time.Now()
	summary := PlexReconcileSummary{}

	r := &reconciler{
		deps:      deps,
		log:       log,
		showCache: map[string]*storage.PlexShow{},
		hdrTopUps: map[string]bool{},
	}

	var annotations []storage.Annotation
	// Every examined torrent has its plex annotations dropped before the fresh
	// verdicts are written, so a re-run cannot leave a stale or contradictory
	// pair behind.
	var examinedIDs []int

	for _, status := range reconcileStatuses {
		torrents, err := deps.Store.List(status, "", "")
		if err != nil {
			return summary, fmt.Errorf("plex reconcile: list %s: %w", status, err)
		}
		for _, torrent := range torrents {
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			summary.Examined++
			examinedIDs = append(examinedIDs, torrent.ID)

			match, err := r.match(ctx, torrent)
			if err != nil {
				summary.Errors = append(summary.Errors, err.Error())
				continue
			}
			if match == nil {
				summary.Skipped++
				continue
			}

			summary.Matched++
			if match.tier == MatchTierGUID {
				summary.GUIDTier++
			} else {
				summary.TitleTier++
			}

			verdict, reason := plex.Compare(
				plex.Copy{
					Quality: torrent.FeedItem.Quality,
					Codec:   torrent.FeedItem.Codec,
					HDR:     torrent.FeedItem.HDR,
				},
				plex.Copy{
					Quality: match.item.Resolution,
					Codec:   match.item.Codec,
					HDR:     match.item.HDR,
				},
			)
			if verdict == plex.VerdictUpgrade {
				summary.Upgrades++
			} else {
				summary.InLibrary++
			}

			detail, err := json.Marshal(PlexAnnotationDetail{
				MatchTier:     match.tier,
				MatchedOn:     match.matchedOn,
				Reason:        reason,
				PlexRatingKey: match.item.RatingKey,
				PlexTitle:     match.item.Title,
				PlexQuality:   match.item.Resolution,
				PlexCodec:     match.item.Codec,
				PlexHDR:       match.item.HDR,
				PlexFileSize:  match.item.FileSize,
			})
			if err != nil {
				summary.Errors = append(summary.Errors, err.Error())
				continue
			}

			// Clear the opposite verdict so a re-run cannot leave both.
			annotations = append(annotations, storage.Annotation{
				TorrentID: torrent.ID,
				Kind:      verdict,
				Source:    storage.AnnotationSourcePlex,
				Detail:    detail,
			})
		}
	}

	cleared, err := deps.Store.DeleteAnnotationsBySource(examinedIDs, storage.AnnotationSourcePlex)
	if err != nil {
		summary.Errors = append(summary.Errors, err.Error())
	}
	summary.Cleared = cleared

	if err := deps.Store.UpsertAnnotations(annotations); err != nil {
		return summary, fmt.Errorf("plex reconcile: write annotations: %w", err)
	}

	summary.DurationMS = time.Since(started).Milliseconds()
	log.Info("plex reconcile complete",
		zap.Int("examined", summary.Examined),
		zap.Int("matched", summary.Matched),
		zap.Int("in_library", summary.InLibrary),
		zap.Int("upgrades", summary.Upgrades),
		zap.Int("guid_tier", summary.GUIDTier),
		zap.Int("title_tier", summary.TitleTier))
	return summary, nil
}

type reconciler struct {
	deps      PlexReconcileDeps
	log       *zap.Logger
	showCache map[string]*storage.PlexShow
	hdrTopUps map[string]bool
}

type plexMatch struct {
	item      *storage.PlexItem
	tier      string
	matchedOn string
}

func (r *reconciler) match(ctx context.Context, torrent models.StagedTorrent) (*plexMatch, error) {
	if torrent.FeedItem.ContentType == models.ContentTypeMovie {
		return r.matchMovie(ctx, torrent)
	}
	return r.matchEpisode(ctx, torrent)
}

func (r *reconciler) matchEpisode(ctx context.Context, torrent models.StagedTorrent) (*plexMatch, error) {
	item := torrent.FeedItem
	// Season packs and unparsed releases have no episode number, so there is
	// nothing to compare against a specific library file.
	if item.ShowName == "" || item.Season == 0 || item.Episode == 0 {
		return nil, nil
	}

	show, tier, matchedOn := r.resolveShow(ctx, item.ShowName)
	if show == nil {
		return nil, nil
	}

	episode, err := r.deps.Store.LookupPlexEpisode(show.RatingKey, item.Season, item.Episode)
	if err != nil {
		return nil, fmt.Errorf("lookup episode for torrent %d: %w", torrent.ID, err)
	}
	if episode == nil {
		return nil, nil
	}
	r.topUpHDR(ctx, episode)
	return &plexMatch{item: episode, tier: tier, matchedOn: matchedOn}, nil
}

func (r *reconciler) matchMovie(ctx context.Context, torrent models.StagedTorrent) (*plexMatch, error) {
	item := torrent.FeedItem
	if item.ShowName == "" {
		return nil, nil
	}

	if meta := r.resolveMeta(ctx, item.ShowName, true); meta != nil && meta.HasExternalIDs() {
		movie, err := r.deps.Store.LookupPlexMovieByIDs(meta.IMDbID, meta.TMDBID, meta.TVDBID)
		if err != nil {
			return nil, fmt.Errorf("lookup movie by ids for torrent %d: %w", torrent.ID, err)
		}
		if movie != nil {
			r.topUpHDR(ctx, movie)
			return &plexMatch{item: movie, tier: MatchTierGUID, matchedOn: idLabel(meta)}, nil
		}
	}

	movie, err := r.deps.Store.LookupPlexMovieByTitle(plex.NormalizeTitle(item.ShowName), item.ReleaseYear)
	if err != nil {
		return nil, fmt.Errorf("lookup movie by title for torrent %d: %w", torrent.ID, err)
	}
	if movie == nil {
		return nil, nil
	}
	r.topUpHDR(ctx, movie)
	return &plexMatch{item: movie, tier: MatchTierTitle, matchedOn: "title"}, nil
}

// resolveShow finds the Plex show for a release, preferring an external id
// match and falling back to the normalized title. Results are memoized because
// many staged torrents share a show within a single run.
func (r *reconciler) resolveShow(ctx context.Context, showName string) (*storage.PlexShow, string, string) {
	norm := plex.NormalizeTitle(showName)
	if cached, ok := r.showCache[norm]; ok {
		if cached == nil {
			return nil, "", ""
		}
		tier, matchedOn := r.showTier(ctx, cached, showName)
		return cached, tier, matchedOn
	}

	var show *storage.PlexShow
	tier, matchedOn := MatchTierTitle, "title"

	if meta := r.resolveMeta(ctx, showName, false); meta != nil && meta.HasExternalIDs() {
		found, err := r.deps.Store.LookupPlexShowByIDs(meta.IMDbID, meta.TMDBID, meta.TVDBID)
		if err != nil {
			r.log.Warn("plex show id lookup failed", zap.String("show", showName), zap.Error(err))
		} else if found != nil {
			show, tier, matchedOn = found, MatchTierGUID, idLabel(meta)
		}
	}

	if show == nil {
		found, err := r.deps.Store.LookupPlexShowByTitle(norm)
		if err != nil {
			r.log.Warn("plex show title lookup failed", zap.String("show", showName), zap.Error(err))
		}
		show = found
	}

	r.showCache[norm] = show
	if show == nil {
		return nil, "", ""
	}
	return show, tier, matchedOn
}

// showTier recomputes the tier for a memoized show without re-querying storage.
func (r *reconciler) showTier(ctx context.Context, show *storage.PlexShow, showName string) (string, string) {
	if meta := r.resolveMeta(ctx, showName, false); meta != nil && meta.HasExternalIDs() {
		if idsOverlap(meta, show) {
			return MatchTierGUID, idLabel(meta)
		}
	}
	return MatchTierTitle, "title"
}

func (r *reconciler) resolveMeta(ctx context.Context, name string, isMovie bool) *metadata.ShowMetadata {
	if r.deps.Metadata == nil {
		return nil
	}
	if isMovie {
		return r.deps.Metadata.ResolveMovie(ctx, name)
	}
	return r.deps.Metadata.Resolve(ctx, name)
}

// topUpHDR fills in HDR for a matched item. Section listings omit per-stream
// data, so it is fetched once per item and written back to the cache.
func (r *reconciler) topUpHDR(ctx context.Context, item *storage.PlexItem) {
	if len(item.HDR) > 0 || r.deps.Plex == nil || r.hdrTopUps[item.RatingKey] {
		return
	}
	r.hdrTopUps[item.RatingKey] = true

	detail, err := r.deps.Plex.Item(ctx, item.RatingKey)
	if err != nil || detail == nil || len(detail.HDR) == 0 {
		return
	}
	item.HDR = detail.HDR
	if err := r.deps.Store.UpdatePlexItemMedia(
		item.RatingKey, item.Resolution, item.Codec, detail.HDR, item.FileSize); err != nil {
		r.log.Warn("plex hdr write-back failed", zap.String("rating_key", item.RatingKey), zap.Error(err))
	}
}

func idsOverlap(meta *metadata.ShowMetadata, show *storage.PlexShow) bool {
	return (meta.IMDbID != "" && meta.IMDbID == show.IMDbID) ||
		(meta.TMDBID != "" && meta.TMDBID == show.TMDBID) ||
		(meta.TVDBID != "" && meta.TVDBID == show.TVDBID)
}

func idLabel(meta *metadata.ShowMetadata) string {
	switch {
	case meta.IMDbID != "":
		return "imdb"
	case meta.TMDBID != "":
		return "tmdb"
	case meta.TVDBID != "":
		return "tvdb"
	default:
		return "title"
	}
}
