package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/storage"
)

// fakePlexServer serves the shapes a live server returns: shows as Directory
// without external ids, episodes as Metadata, and ids only on item detail.
type fakePlexServer struct {
	mu          sync.Mutex
	detailCalls []string
}

func (f *fakePlexServer) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		itemType := r.URL.Query().Get("type")

		switch {
		case path == "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
				{"key":"1","title":"Television","type":"show"},
				{"key":"4","title":"Movies","type":"movie"},
				{"key":"9","title":"Music","type":"artist"}
			]}}`))

		case strings.HasPrefix(path, "/library/metadata/"):
			key := strings.TrimPrefix(path, "/library/metadata/")
			f.mu.Lock()
			f.detailCalls = append(f.detailCalls, key)
			f.mu.Unlock()
			writeDetail(w, key)

		case path == "/library/sections/1/all" && itemType == "2":
			pageOut(w, r, []map[string]any{
				{"ratingKey": "17784", "type": "show", "title": "MobLand", "year": 2025},
				{"ratingKey": "16569", "type": "show", "title": "Shōgun", "originalTitle": "Shogun", "year": 2024},
			}, "Directory")

		case path == "/library/sections/1/all" && itemType == "4":
			pageOut(w, r, []map[string]any{
				{
					"ratingKey": "24256", "type": "episode", "grandparentRatingKey": "17784",
					"title": "I Wanna Be Your Dog", "parentIndex": 2, "index": 1,
					"Media": []map[string]any{{
						"videoResolution": "4k", "videoCodec": "hevc", "duration": 2934556,
						"Part": []map[string]any{{"size": 9351276799, "file": "/media/MobLand S02E01.mkv"}},
					}},
				},
			}, "Metadata")

		case path == "/library/sections/4/all" && itemType == "1":
			pageOut(w, r, []map[string]any{
				{
					"ratingKey": "17997", "type": "movie", "title": "Amadeus", "year": 1984,
					"Media": []map[string]any{{
						"videoResolution": "1080", "videoCodec": "vc1", "duration": 10825696,
						"Part": []map[string]any{{"size": 25890801371, "file": "/media/Amadeus.mkv"}},
					}},
				},
			}, "Metadata")

		default:
			t.Errorf("unexpected request %s?%s", path, r.URL.RawQuery)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func writeDetail(w http.ResponseWriter, ratingKey string) {
	ids := map[string][]map[string]string{
		"17784": {{"id": "imdb://tt31566242"}, {"id": "tmdb://249042"}, {"id": "tvdb://446618"}},
		"16569": {{"id": "tvdb://386818"}},
		"17997": {{"id": "imdb://tt0086879"}, {"id": "tmdb://279"}},
	}[ratingKey]

	_ = json.NewEncoder(w).Encode(map[string]any{
		"MediaContainer": map[string]any{
			"Metadata": []map[string]any{{"ratingKey": ratingKey, "Guid": ids}},
		},
	})
}

// pageOut honours the container headers so the pagination loop terminates.
func pageOut(w http.ResponseWriter, r *http.Request, all []map[string]any, key string) {
	start, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
	var page []map[string]any
	if start < len(all) {
		page = all[start:]
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"MediaContainer": map[string]any{"size": len(page), key: page},
	})
}

func newSyncTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	s, err := storage.New(filepath.Join(t.TempDir(), "curator.db"))
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRunPlexSync(t *testing.T) {
	fake := &fakePlexServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	px, err := client.NewPlex(client.PlexConfig{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("NewPlex: %v", err)
	}
	store := newSyncTestStore(t)

	summary, err := RunPlexSync(context.Background(), PlexSyncConfig{},
		PlexSyncDeps{Store: store, Plex: px})
	if err != nil {
		t.Fatalf("RunPlexSync: %v", err)
	}

	// The music section must be ignored.
	if summary.Sections != 2 {
		t.Errorf("Sections = %d, want 2", summary.Sections)
	}
	if summary.Shows != 2 || summary.Episodes != 1 || summary.Movies != 1 {
		t.Errorf("summary = %+v", summary)
	}

	// Listings carry no ids, so each show and movie needs one detail call.
	if summary.GUIDLookup != 3 {
		t.Errorf("GUIDLookup = %d, want 3", summary.GUIDLookup)
	}

	show, err := store.LookupPlexShowByIDs("", "", "446618")
	if err != nil {
		t.Fatalf("LookupPlexShowByIDs: %v", err)
	}
	if show == nil || show.RatingKey != "17784" {
		t.Fatalf("show by tvdb id = %+v", show)
	}

	ep, err := store.LookupPlexEpisode("17784", 2, 1)
	if err != nil {
		t.Fatalf("LookupPlexEpisode: %v", err)
	}
	if ep == nil {
		t.Fatal("episode not linked to its show via grandparentRatingKey")
	}
	if ep.Resolution != "2160P" || ep.Codec != "x265" {
		t.Errorf("episode media = %+v", ep)
	}

	movie, err := store.LookupPlexMovieByIDs("tt0086879", "", "")
	if err != nil {
		t.Fatalf("LookupPlexMovieByIDs: %v", err)
	}
	if movie == nil || movie.Resolution != "1080P" {
		t.Errorf("movie = %+v", movie)
	}
}

func TestRunPlexSyncSkipsResolvedGUIDsOnRerun(t *testing.T) {
	fake := &fakePlexServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	px, _ := client.NewPlex(client.PlexConfig{BaseURL: srv.URL, Token: "t"})
	store := newSyncTestStore(t)
	deps := PlexSyncDeps{Store: store, Plex: px}

	first, err := RunPlexSync(context.Background(), PlexSyncConfig{}, deps)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	second, err := RunPlexSync(context.Background(), PlexSyncConfig{}, deps)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}

	if first.GUIDLookup == 0 {
		t.Fatal("first sync should have resolved ids")
	}
	// Steady state must not re-fetch ids that are already cached.
	if second.GUIDLookup != 0 {
		t.Errorf("second sync made %d detail calls, want 0", second.GUIDLookup)
	}
	if second.Removed != 0 {
		t.Errorf("Removed = %d, want 0 — nothing left the library", second.Removed)
	}
}

func TestRunPlexSyncMatchesOriginalTitle(t *testing.T) {
	fake := &fakePlexServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	px, _ := client.NewPlex(client.PlexConfig{BaseURL: srv.URL, Token: "t"})
	store := newSyncTestStore(t)

	if _, err := RunPlexSync(context.Background(), PlexSyncConfig{},
		PlexSyncDeps{Store: store, Plex: px}); err != nil {
		t.Fatalf("RunPlexSync: %v", err)
	}

	// "Shōgun" is stored with "shogun" as an alias, so a release named
	// Shogun.S01E01 resolves without needing the macron.
	got, err := store.LookupPlexShowByTitle("shogun")
	if err != nil {
		t.Fatalf("LookupPlexShowByTitle: %v", err)
	}
	if got == nil || got.RatingKey != "16569" {
		t.Errorf("show by original title = %+v", got)
	}
}

func TestRunPlexSyncSectionFilter(t *testing.T) {
	fake := &fakePlexServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	px, _ := client.NewPlex(client.PlexConfig{BaseURL: srv.URL, Token: "t"})
	store := newSyncTestStore(t)

	summary, err := RunPlexSync(context.Background(),
		PlexSyncConfig{SectionKeys: []string{"4"}},
		PlexSyncDeps{Store: store, Plex: px})
	if err != nil {
		t.Fatalf("RunPlexSync: %v", err)
	}
	if summary.Sections != 1 || summary.Movies != 1 || summary.Shows != 0 {
		t.Errorf("summary = %+v, want only the movie section", summary)
	}
}

func TestRunPlexSyncNilClient(t *testing.T) {
	_, err := RunPlexSync(context.Background(), PlexSyncConfig{},
		PlexSyncDeps{Store: newSyncTestStore(t)})
	if err == nil {
		t.Fatal("expected an error when the Plex client is unavailable")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("err = %v", err)
	}
}

func TestRunPlexSyncCancelledContext(t *testing.T) {
	fake := &fakePlexServer{}
	srv := httptest.NewServer(fake.handler(t))
	defer srv.Close()

	px, _ := client.NewPlex(client.PlexConfig{BaseURL: srv.URL, Token: "t"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := RunPlexSync(ctx, PlexSyncConfig{},
		PlexSyncDeps{Store: newSyncTestStore(t), Plex: px}); err == nil {
		t.Fatal("expected a context error")
	}
}
