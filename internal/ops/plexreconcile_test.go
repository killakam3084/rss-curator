package ops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
)

// stubResolver returns canned metadata keyed by lowercase title.
type stubResolver struct {
	shows  map[string]*metadata.ShowMetadata
	movies map[string]*metadata.ShowMetadata
	calls  int
}

func (s *stubResolver) Resolve(_ context.Context, name string) *metadata.ShowMetadata {
	s.calls++
	return s.shows[name]
}

func (s *stubResolver) ResolveMovie(_ context.Context, name string) *metadata.ShowMetadata {
	s.calls++
	return s.movies[name]
}

func seedLibrary(t *testing.T, store *storage.Storage) {
	t.Helper()
	now := time.Now().UTC()

	if err := store.UpsertPlexShows([]storage.PlexShow{{
		RatingKey: "17784", SectionKey: "1", Title: "MobLand", NormTitle: "mobland",
		Year: 2025, IMDbID: "tt31566242", TMDBID: "249042", TVDBID: "446618", SyncedAt: now,
	}, {
		RatingKey: "211", SectionKey: "1", Title: "The Bear", NormTitle: "bear",
		Year: 2022, SyncedAt: now,
	}}); err != nil {
		t.Fatalf("UpsertPlexShows: %v", err)
	}

	if err := store.UpsertPlexItems([]storage.PlexItem{{
		RatingKey: "24256", SectionKey: "1", ContentType: "episode", ShowRatingKey: "17784",
		Title: "I Wanna Be Your Dog", Season: 2, Episode: 1,
		Resolution: "2160P", Codec: "x265", HDR: []string{"dv", "hdr10plus"},
		FileSize: 9351276799, SyncedAt: now,
	}, {
		RatingKey: "1001", SectionKey: "1", ContentType: "episode", ShowRatingKey: "211",
		Title: "System", Season: 1, Episode: 1,
		Resolution: "1080P", Codec: "x264", SyncedAt: now,
	}, {
		RatingKey: "17997", SectionKey: "4", ContentType: "movie",
		Title: "Amadeus", NormTitle: "amadeus", Year: 1984,
		IMDbID: "tt0086879", TMDBID: "279",
		Resolution: "1080P", Codec: "vc1", SyncedAt: now,
	}}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}
}

func stageTorrent(t *testing.T, store *storage.Storage, item models.FeedItem) int {
	t.Helper()
	if err := store.Add(models.StagedTorrent{
		FeedItem: item, MatchReason: "test", Status: "pending",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	torrents, err := store.List("", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tr := range torrents {
		if tr.FeedItem.Link == item.Link {
			return tr.ID
		}
	}
	t.Fatalf("staged torrent %q not found", item.Link)
	return 0
}

func annotationFor(t *testing.T, store *storage.Storage, id int) (storage.Annotation, PlexAnnotationDetail) {
	t.Helper()
	all, err := store.AnnotationsForTorrents([]int{id})
	if err != nil {
		t.Fatalf("AnnotationsForTorrents: %v", err)
	}
	if len(all[id]) != 1 {
		t.Fatalf("torrent %d has %d annotations, want 1", id, len(all[id]))
	}
	ann := all[id][0]
	var detail PlexAnnotationDetail
	if err := json.Unmarshal(ann.Detail, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return ann, detail
}

func TestReconcileEpisodeMatchesByGUID(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "MobLand S02E01 1080p WEB-DL x265-FLUX", Link: "magnet:?guid-episode",
		ContentType: models.ContentTypeShow, ShowName: "MobLand", Season: 2, Episode: 1,
		Quality: "1080P", Codec: "x265",
	})

	resolver := &stubResolver{shows: map[string]*metadata.ShowMetadata{
		"MobLand": {TVDBID: "446618", IMDbID: "tt31566242"},
	}}

	summary, err := RunPlexReconcile(context.Background(),
		PlexReconcileDeps{Store: store, Metadata: resolver})
	if err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}
	if summary.Matched != 1 || summary.GUIDTier != 1 || summary.InLibrary != 1 {
		t.Fatalf("summary = %+v", summary)
	}

	ann, detail := annotationFor(t, store, id)
	if ann.Kind != storage.AnnotationInLibrary {
		t.Errorf("kind = %q, want in_library — 1080p does not beat the 2160p copy", ann.Kind)
	}
	if detail.MatchTier != MatchTierGUID || detail.MatchedOn != "imdb" {
		t.Errorf("detail = %+v", detail)
	}
	if detail.PlexQuality != "2160P" || len(detail.PlexHDR) != 2 {
		t.Errorf("detail did not carry the library copy's media: %+v", detail)
	}
}

func TestReconcileEpisodeUpgradeVerdict(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "The Bear S01E01 2160p WEB-DL x265", Link: "magnet:?upgrade",
		ContentType: models.ContentTypeShow, ShowName: "The Bear", Season: 1, Episode: 1,
		Quality: "2160P", Codec: "x265",
	})

	summary, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store})
	if err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}
	if summary.Upgrades != 1 {
		t.Fatalf("summary = %+v, want one upgrade", summary)
	}

	ann, detail := annotationFor(t, store, id)
	if ann.Kind != storage.AnnotationLibraryUpgrade {
		t.Errorf("kind = %q, want library_upgrade", ann.Kind)
	}
	// No metadata resolver was supplied, so this can only be a title match.
	if detail.MatchTier != MatchTierTitle {
		t.Errorf("MatchTier = %q, want title", detail.MatchTier)
	}
}

func TestReconcileMovieMatchesByGUID(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "Amadeus 1984 2160p BluRay x265", Link: "magnet:?movie",
		ContentType: models.ContentTypeMovie, ShowName: "Amadeus", ReleaseYear: 1984,
		Quality: "2160P", Codec: "x265",
	})

	resolver := &stubResolver{movies: map[string]*metadata.ShowMetadata{
		"Amadeus": {IMDbID: "tt0086879", TMDBID: "279"},
	}}

	if _, err := RunPlexReconcile(context.Background(),
		PlexReconcileDeps{Store: store, Metadata: resolver}); err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}

	ann, detail := annotationFor(t, store, id)
	if ann.Kind != storage.AnnotationLibraryUpgrade {
		t.Errorf("kind = %q, want library_upgrade over the 1080p remux", ann.Kind)
	}
	if detail.MatchTier != MatchTierGUID {
		t.Errorf("MatchTier = %q, want guid", detail.MatchTier)
	}
}

func TestReconcileSkipsSeasonPacksAndUnknownShows(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	// Season pack: no episode number, so there is nothing to compare.
	stageTorrent(t, store, models.FeedItem{
		Title: "MobLand S02 1080p WEB-DL", Link: "magnet:?pack",
		ContentType: models.ContentTypeShow, ShowName: "MobLand", Season: 2, Quality: "1080P",
	})
	// Show that is not in the library at all.
	stageTorrent(t, store, models.FeedItem{
		Title: "Some Unknown Show S01E01 1080p", Link: "magnet:?unknown",
		ContentType: models.ContentTypeShow, ShowName: "Some Unknown Show",
		Season: 1, Episode: 1, Quality: "1080P",
	})
	// Episode the library does not hold.
	stageTorrent(t, store, models.FeedItem{
		Title: "MobLand S02E09 1080p", Link: "magnet:?missing-ep",
		ContentType: models.ContentTypeShow, ShowName: "MobLand",
		Season: 2, Episode: 9, Quality: "1080P",
	})

	summary, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store})
	if err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}
	if summary.Examined != 3 || summary.Matched != 0 || summary.Skipped != 3 {
		t.Errorf("summary = %+v", summary)
	}

	counts, err := store.CountAnnotationsByKind()
	if err != nil {
		t.Fatalf("CountAnnotationsByKind: %v", err)
	}
	if len(counts) != 0 {
		t.Errorf("counts = %+v, want no annotations", counts)
	}
}

func TestReconcileClearsStaleAnnotations(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "The Bear S01E01 720p", Link: "magnet:?stale",
		ContentType: models.ContentTypeShow, ShowName: "The Bear", Season: 1, Episode: 1,
		Quality: "720P",
	})

	if _, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if ann, _ := annotationFor(t, store, id); ann.Kind != storage.AnnotationInLibrary {
		t.Fatalf("kind = %q, want in_library", ann.Kind)
	}

	// Drop the episode from the library and re-run; the annotation must go.
	if _, err := store.DeleteStalePlexRows("1", time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatalf("DeleteStalePlexRows: %v", err)
	}
	if _, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	all, err := store.AnnotationsForTorrents([]int{id})
	if err != nil {
		t.Fatalf("AnnotationsForTorrents: %v", err)
	}
	if len(all[id]) != 0 {
		t.Errorf("annotations = %+v, want none after the episode left the library", all[id])
	}
}

func TestReconcileVerdictFlipsWithoutDuplicating(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "The Bear S01E01 2160p", Link: "magnet:?flip",
		ContentType: models.ContentTypeShow, ShowName: "The Bear", Season: 1, Episode: 1,
		Quality: "2160P",
	})

	if _, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store}); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if ann, _ := annotationFor(t, store, id); ann.Kind != storage.AnnotationLibraryUpgrade {
		t.Fatalf("kind = %q, want library_upgrade", ann.Kind)
	}

	// The library gains a 2160p copy, so the same torrent is now a duplicate.
	if err := store.UpsertPlexItems([]storage.PlexItem{{
		RatingKey: "1001", SectionKey: "1", ContentType: "episode", ShowRatingKey: "211",
		Title: "System", Season: 1, Episode: 1, Resolution: "2160P",
		SyncedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}
	if _, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store}); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	// annotationFor asserts exactly one row, which is the real check here.
	if ann, _ := annotationFor(t, store, id); ann.Kind != storage.AnnotationInLibrary {
		t.Errorf("kind = %q, want in_library after the upgrade landed", ann.Kind)
	}
}

func TestReconcileMemoizesShowResolution(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	for _, ep := range []int{1, 2, 3} {
		stageTorrent(t, store, models.FeedItem{
			Title:       "MobLand S02E0" + string(rune('0'+ep)),
			Link:        "magnet:?ep" + string(rune('0'+ep)),
			ContentType: models.ContentTypeShow, ShowName: "MobLand",
			Season: 2, Episode: ep, Quality: "1080P",
		})
	}

	resolver := &stubResolver{shows: map[string]*metadata.ShowMetadata{
		"MobLand": {TVDBID: "446618"},
	}}
	if _, err := RunPlexReconcile(context.Background(),
		PlexReconcileDeps{Store: store, Metadata: resolver}); err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}

	// One resolve for the first torrent, then one per cache hit to recompute
	// the tier — never a storage lookup per torrent.
	if resolver.calls > 3 {
		t.Errorf("resolver called %d times for 3 episodes of one show", resolver.calls)
	}
}

func TestReconcileWithoutMetadataFallsBackToTitle(t *testing.T) {
	store := newSyncTestStore(t)
	seedLibrary(t, store)

	id := stageTorrent(t, store, models.FeedItem{
		Title: "MobLand S02E01 1080p", Link: "magnet:?notmeta",
		ContentType: models.ContentTypeShow, ShowName: "MobLand",
		Season: 2, Episode: 1, Quality: "1080P",
	})

	summary, err := RunPlexReconcile(context.Background(), PlexReconcileDeps{Store: store})
	if err != nil {
		t.Fatalf("RunPlexReconcile: %v", err)
	}
	if summary.TitleTier != 1 || summary.GUIDTier != 0 {
		t.Errorf("summary = %+v, want a title-tier match", summary)
	}
	if _, detail := annotationFor(t, store, id); detail.MatchTier != MatchTierTitle {
		t.Errorf("MatchTier = %q", detail.MatchTier)
	}
}
