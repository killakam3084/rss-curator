package storage

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
)

func newPlexTestStore(t *testing.T) *Storage {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "curator.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUpsertAndLookupPlexShowByIDs(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	shows := []PlexShow{{
		RatingKey: "17784", SectionKey: "1", Title: "MobLand", NormTitle: "mobland",
		Year: 2025, IMDbID: "tt31566242", TMDBID: "249042", TVDBID: "446618", SyncedAt: now,
	}}
	if err := s.UpsertPlexShows(shows); err != nil {
		t.Fatalf("UpsertPlexShows: %v", err)
	}

	// Any single id should resolve the show.
	for _, tc := range []struct{ imdb, tmdb, tvdb string }{
		{"tt31566242", "", ""},
		{"", "249042", ""},
		{"", "", "446618"},
		{"no-match", "249042", ""},
	} {
		got, err := s.LookupPlexShowByIDs(tc.imdb, tc.tmdb, tc.tvdb)
		if err != nil {
			t.Fatalf("LookupPlexShowByIDs(%v): %v", tc, err)
		}
		if got == nil || got.RatingKey != "17784" {
			t.Errorf("LookupPlexShowByIDs(%v) = %+v, want rating key 17784", tc, got)
		}
	}

	// No ids supplied must not match everything.
	got, err := s.LookupPlexShowByIDs("", "", "")
	if err != nil {
		t.Fatalf("LookupPlexShowByIDs(empty): %v", err)
	}
	if got != nil {
		t.Errorf("LookupPlexShowByIDs with no ids = %+v, want nil", got)
	}

	// Unknown ids must miss.
	if got, _ := s.LookupPlexShowByIDs("tt0000000", "", ""); got != nil {
		t.Errorf("unknown id matched %+v", got)
	}
}

func TestUpsertPlexShowsIsIdempotent(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC()

	show := PlexShow{RatingKey: "1", SectionKey: "1", Title: "Old", NormTitle: "old", SyncedAt: now}
	if err := s.UpsertPlexShows([]PlexShow{show}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	show.Title = "New"
	show.NormTitle = "new"
	show.TVDBID = "999"
	if err := s.UpsertPlexShows([]PlexShow{show}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	counts, err := s.GetPlexCounts()
	if err != nil {
		t.Fatalf("GetPlexCounts: %v", err)
	}
	if counts.Shows != 1 {
		t.Errorf("Shows = %d, want 1 after re-upsert", counts.Shows)
	}
	got, _ := s.LookupPlexShowByIDs("", "", "999")
	if got == nil || got.Title != "New" {
		t.Errorf("row not updated: %+v", got)
	}
}

func TestLookupPlexEpisode(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC()

	items := []PlexItem{{
		RatingKey: "24256", SectionKey: "1", ContentType: "episode", ShowRatingKey: "17784",
		Title: "I Wanna Be Your Dog", NormTitle: "i wanna be your dog",
		Season: 2, Episode: 1, Resolution: "2160P", Codec: "x265",
		HDR: []string{"dv", "hdr10plus"}, FileSize: 9351276799,
		FilePath: "/mnt/media/MobLand S02E01.mkv", SyncedAt: now,
	}}
	if err := s.UpsertPlexItems(items); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}

	got, err := s.LookupPlexEpisode("17784", 2, 1)
	if err != nil {
		t.Fatalf("LookupPlexEpisode: %v", err)
	}
	if got == nil {
		t.Fatal("LookupPlexEpisode returned nil")
	}
	if got.Resolution != "2160P" || got.Codec != "x265" {
		t.Errorf("media fields = %+v", got)
	}
	if len(got.HDR) != 2 || got.HDR[0] != "dv" || got.HDR[1] != "hdr10plus" {
		t.Errorf("HDR = %v, want [dv hdr10plus] round-tripped", got.HDR)
	}

	// Wrong episode, wrong season, and unknown show must all miss.
	for _, tc := range []struct {
		show    string
		s, e    int
		comment string
	}{
		{"17784", 2, 2, "wrong episode"},
		{"17784", 1, 1, "wrong season"},
		{"99999", 2, 1, "unknown show"},
		{"", 2, 1, "no show key"},
	} {
		if got, _ := s.LookupPlexEpisode(tc.show, tc.s, tc.e); got != nil {
			t.Errorf("%s matched %+v, want nil", tc.comment, got)
		}
	}
}

func TestLookupPlexMovieByTitleYearDrift(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC()

	if err := s.UpsertPlexItems([]PlexItem{{
		RatingKey: "m1", SectionKey: "2", ContentType: "movie",
		Title: "Dune: Part Two", NormTitle: "dune part two", Year: 2024,
		Resolution: "2160P", SyncedAt: now,
	}}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}

	for _, year := range []int{2023, 2024, 2025} {
		got, err := s.LookupPlexMovieByTitle("dune part two", year)
		if err != nil {
			t.Fatalf("LookupPlexMovieByTitle(%d): %v", year, err)
		}
		if got == nil {
			t.Errorf("year %d did not match within the allowed drift", year)
		}
	}
	if got, _ := s.LookupPlexMovieByTitle("dune part two", 2020); got != nil {
		t.Errorf("year 2020 matched %+v, want nil beyond the drift window", got)
	}
	if got, _ := s.LookupPlexMovieByTitle("dune part two", 0); got == nil {
		t.Error("unknown year should still match on title alone")
	}
	if got, _ := s.LookupPlexMovieByTitle("", 2024); got != nil {
		t.Error("empty title must not match")
	}
}

func TestLookupPlexMovieByIDsIgnoresEpisodes(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC()

	if err := s.UpsertPlexItems([]PlexItem{
		{RatingKey: "e1", SectionKey: "1", ContentType: "episode", IMDbID: "tt1", SyncedAt: now},
		{RatingKey: "m1", SectionKey: "2", ContentType: "movie", IMDbID: "tt2", SyncedAt: now},
	}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}

	if got, _ := s.LookupPlexMovieByIDs("tt1", "", ""); got != nil {
		t.Errorf("episode returned from movie lookup: %+v", got)
	}
	got, _ := s.LookupPlexMovieByIDs("tt2", "", "")
	if got == nil || got.RatingKey != "m1" {
		t.Errorf("LookupPlexMovieByIDs = %+v, want m1", got)
	}
}

func TestUpdatePlexItemMedia(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC()

	if err := s.UpsertPlexItems([]PlexItem{{
		RatingKey: "e1", SectionKey: "1", ContentType: "episode", ShowRatingKey: "s1",
		Season: 1, Episode: 1, Resolution: "2160P", SyncedAt: now,
	}}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}

	if err := s.UpdatePlexItemMedia("e1", "2160P", "x265", []string{"dv"}, 123); err != nil {
		t.Fatalf("UpdatePlexItemMedia: %v", err)
	}
	got, _ := s.LookupPlexEpisode("s1", 1, 1)
	if got == nil || len(got.HDR) != 1 || got.HDR[0] != "dv" || got.FileSize != 123 {
		t.Errorf("media not updated: %+v", got)
	}
}

func TestDeleteStalePlexRows(t *testing.T) {
	s := newPlexTestStore(t)
	old := time.Now().UTC().Add(-time.Hour)
	fresh := time.Now().UTC()

	if err := s.UpsertPlexShows([]PlexShow{
		{RatingKey: "s-old", SectionKey: "1", SyncedAt: old},
		{RatingKey: "s-new", SectionKey: "1", SyncedAt: fresh},
		{RatingKey: "s-other", SectionKey: "2", SyncedAt: old},
	}); err != nil {
		t.Fatalf("UpsertPlexShows: %v", err)
	}
	if err := s.UpsertPlexItems([]PlexItem{
		{RatingKey: "i-old", SectionKey: "1", ContentType: "episode", SyncedAt: old},
		{RatingKey: "i-new", SectionKey: "1", ContentType: "episode", SyncedAt: fresh},
	}); err != nil {
		t.Fatalf("UpsertPlexItems: %v", err)
	}

	deleted, err := s.DeleteStalePlexRows("1", fresh.Add(-time.Minute))
	if err != nil {
		t.Fatalf("DeleteStalePlexRows: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (one show, one item)", deleted)
	}

	counts, _ := s.GetPlexCounts()
	if counts.Shows != 2 {
		t.Errorf("Shows = %d, want 2 (fresh row plus the untouched other section)", counts.Shows)
	}
	if counts.Episodes != 1 {
		t.Errorf("Episodes = %d, want 1", counts.Episodes)
	}
}

func TestPlexLibraries(t *testing.T) {
	s := newPlexTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.UpsertPlexLibrary(PlexLibrary{
		SectionKey: "1", Title: "Television", Type: "show", ItemCount: 10, LastSyncedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPlexLibrary: %v", err)
	}
	if err := s.UpsertPlexLibrary(PlexLibrary{
		SectionKey: "1", Title: "Television", Type: "show", ItemCount: 42, LastSyncedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPlexLibrary update: %v", err)
	}

	libs, err := s.ListPlexLibraries()
	if err != nil {
		t.Fatalf("ListPlexLibraries: %v", err)
	}
	if len(libs) != 1 || libs[0].ItemCount != 42 {
		t.Errorf("libs = %+v, want a single row with count 42", libs)
	}
}

func TestGetPlexCountsEmpty(t *testing.T) {
	s := newPlexTestStore(t)
	counts, err := s.GetPlexCounts()
	if err != nil {
		t.Fatalf("GetPlexCounts: %v", err)
	}
	if counts.Shows != 0 || counts.Episodes != 0 || counts.Movies != 0 {
		t.Errorf("counts = %+v, want all zero", counts)
	}
}

func TestAnnotationLifecycle(t *testing.T) {
	s := newPlexTestStore(t)
	id1 := addAnnotationTestTorrent(t, s, "magnet:?one")
	id2 := addAnnotationTestTorrent(t, s, "magnet:?two")

	err := s.UpsertAnnotations([]Annotation{
		{TorrentID: id1, Kind: AnnotationInLibrary, Source: AnnotationSourcePlex,
			Detail: json.RawMessage(`{"match_tier":"guid"}`)},
		{TorrentID: id2, Kind: AnnotationLibraryUpgrade, Source: AnnotationSourcePlex},
	})
	if err != nil {
		t.Fatalf("UpsertAnnotations: %v", err)
	}

	got, err := s.AnnotationsForTorrents([]int{id1, id2})
	if err != nil {
		t.Fatalf("AnnotationsForTorrents: %v", err)
	}
	if len(got[id1]) != 1 || got[id1][0].Kind != AnnotationInLibrary {
		t.Errorf("torrent %d annotations = %+v", id1, got[id1])
	}
	if string(got[id1][0].Detail) != `{"match_tier":"guid"}` {
		t.Errorf("detail = %s", got[id1][0].Detail)
	}
	// A missing detail must still be valid JSON for the API layer.
	if string(got[id2][0].Detail) != "{}" {
		t.Errorf("empty detail = %s, want {}", got[id2][0].Detail)
	}

	// Re-upserting the same (torrent, kind) replaces rather than duplicates.
	if err := s.UpsertAnnotations([]Annotation{
		{TorrentID: id1, Kind: AnnotationInLibrary, Source: AnnotationSourcePlex,
			Detail: json.RawMessage(`{"match_tier":"title"}`)},
	}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ = s.AnnotationsForTorrents([]int{id1})
	if len(got[id1]) != 1 || string(got[id1][0].Detail) != `{"match_tier":"title"}` {
		t.Errorf("re-upsert did not replace: %+v", got[id1])
	}

	counts, err := s.CountAnnotationsByKind()
	if err != nil {
		t.Fatalf("CountAnnotationsByKind: %v", err)
	}
	if counts[AnnotationInLibrary] != 1 || counts[AnnotationLibraryUpgrade] != 1 {
		t.Errorf("counts = %+v", counts)
	}

	deleted, err := s.DeleteAnnotationsBySource([]int{id1}, AnnotationSourcePlex)
	if err != nil {
		t.Fatalf("DeleteAnnotationsBySource: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
	got, _ = s.AnnotationsForTorrents([]int{id1, id2})
	if len(got[id1]) != 0 {
		t.Errorf("torrent %d still annotated", id1)
	}
	if len(got[id2]) != 1 {
		t.Errorf("torrent %d annotation was wrongly removed", id2)
	}
}

func TestAnnotationsForTorrentsEmptyInput(t *testing.T) {
	s := newPlexTestStore(t)
	got, err := s.AnnotationsForTorrents(nil)
	if err != nil {
		t.Fatalf("AnnotationsForTorrents(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want an empty map", got)
	}
}

func TestDeleteAnnotationsBySourceAllTorrents(t *testing.T) {
	s := newPlexTestStore(t)
	id := addAnnotationTestTorrent(t, s, "magnet:?solo")

	if err := s.UpsertAnnotations([]Annotation{
		{TorrentID: id, Kind: AnnotationInLibrary, Source: AnnotationSourcePlex},
	}); err != nil {
		t.Fatalf("UpsertAnnotations: %v", err)
	}
	if _, err := s.DeleteAnnotationsBySource(nil, AnnotationSourcePlex); err != nil {
		t.Fatalf("DeleteAnnotationsBySource: %v", err)
	}
	counts, _ := s.CountAnnotationsByKind()
	if len(counts) != 0 {
		t.Errorf("counts = %+v, want empty", counts)
	}
}

func addAnnotationTestTorrent(t *testing.T, s *Storage, link string) int {
	t.Helper()
	err := s.Add(models.StagedTorrent{
		FeedItem:    models.FeedItem{Title: "Test " + link, Link: link},
		MatchReason: "test",
		Status:      "pending",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	torrents, err := s.List("", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tr := range torrents {
		if tr.FeedItem.Link == link {
			return tr.ID
		}
	}
	t.Fatalf("staged torrent %q not found", link)
	return 0
}
