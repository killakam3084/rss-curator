package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTMDbFetchExtractsExternalIDs(t *testing.T) {
	var detailQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/3/search/tv"):
			_, _ = w.Write([]byte(`{"results":[{"id":1399,"name":"Game of Thrones"}]}`))
		case strings.HasPrefix(r.URL.Path, "/3/tv/1399"):
			detailQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{
				"id":1399,
				"name":"Game of Thrones",
				"first_air_date":"2011-04-17",
				"external_ids":{"imdb_id":"tt0944947","tvdb_id":121361}
			}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := newTMDbProvider(srv.URL, "test-key")
	meta, err := p.Fetch(context.Background(), "Game of Thrones")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if meta == nil {
		t.Fatal("Fetch returned nil metadata")
	}
	if meta.Provider != "tmdb" {
		t.Errorf("Provider = %q, want tmdb", meta.Provider)
	}
	if meta.TMDBID != "1399" {
		t.Errorf("TMDBID = %q, want 1399", meta.TMDBID)
	}
	if meta.TVDBID != "121361" {
		t.Errorf("TVDBID = %q, want 121361", meta.TVDBID)
	}
	if meta.IMDbID != "tt0944947" {
		t.Errorf("IMDbID = %q, want tt0944947", meta.IMDbID)
	}
	if !strings.Contains(detailQuery, "external_ids") {
		t.Errorf("tv detail query %q missing external_ids", detailQuery)
	}
	if !meta.HasExternalIDs() {
		t.Error("HasExternalIDs = false, want true")
	}
}

func TestTMDbFetchTVMissingExternalIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/3/search/tv") {
			_, _ = w.Write([]byte(`{"results":[{"id":7,"name":"Obscure Show"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":7,"name":"Obscure Show","external_ids":{"imdb_id":"","tvdb_id":null}}`))
	}))
	defer srv.Close()

	meta, err := newTMDbProvider(srv.URL, "k").Fetch(context.Background(), "Obscure Show")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if meta.TVDBID != "" {
		t.Errorf("TVDBID = %q, want empty for null tvdb_id", meta.TVDBID)
	}
	if meta.TMDBID != "7" {
		t.Errorf("TMDBID = %q, want 7", meta.TMDBID)
	}
}

func TestTMDbFetchMovieRequestsExternalIDs(t *testing.T) {
	var detailQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/3/search/movie") {
			_, _ = w.Write([]byte(`{"results":[{"id":438631,"title":"Dune"}]}`))
			return
		}
		detailQuery = r.URL.RawQuery
		// imdb_id deliberately absent from the native field to exercise the fallback.
		_, _ = w.Write([]byte(`{
			"id":438631,
			"title":"Dune",
			"release_date":"2021-09-15",
			"external_ids":{"imdb_id":"tt1160419"}
		}`))
	}))
	defer srv.Close()

	meta, err := newTMDbProvider(srv.URL, "k").FetchMovie(context.Background(), "Dune")
	if err != nil {
		t.Fatalf("FetchMovie: %v", err)
	}
	if !strings.Contains(detailQuery, "external_ids") {
		t.Errorf("movie detail query %q missing external_ids", detailQuery)
	}
	if meta.IMDbID != "tt1160419" {
		t.Errorf("IMDbID = %q, want tt1160419 from external_ids fallback", meta.IMDbID)
	}
	if meta.TMDBID != "438631" {
		t.Errorf("TMDBID = %q, want 438631", meta.TMDBID)
	}
	if meta.TVDBID != "" {
		t.Errorf("TVDBID = %q, want empty for movies", meta.TVDBID)
	}
}

func TestTVMazeFetchExtractsExternals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/shows/") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{
			"id":82,
			"name":"Game of Thrones",
			"url":"https://www.tvmaze.com/shows/82",
			"premiered":"2011-04-17",
			"externals":{"tvrage":24493,"thetvdb":121361,"imdb":"tt0944947"}
		}`))
	}))
	defer srv.Close()

	meta, err := newTVMazeProvider(srv.URL).Fetch(context.Background(), "Game of Thrones")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if meta.Provider != "tvmaze" {
		t.Errorf("Provider = %q, want tvmaze", meta.Provider)
	}
	if meta.IMDbID != "tt0944947" {
		t.Errorf("IMDbID = %q, want tt0944947", meta.IMDbID)
	}
	if meta.TVDBID != "121361" {
		t.Errorf("TVDBID = %q, want 121361", meta.TVDBID)
	}
	if meta.TMDBID != "" {
		t.Errorf("TMDBID = %q, want empty (tvmaze has no tmdb id)", meta.TMDBID)
	}
}

func TestTVMazeFetchNullExternals(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/shows/") {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`{"id":9,"name":"Nameless","externals":{"thetvdb":null,"imdb":null}}`))
	}))
	defer srv.Close()

	meta, err := newTVMazeProvider(srv.URL).Fetch(context.Background(), "Nameless")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if meta.TVDBID != "" || meta.IMDbID != "" {
		t.Errorf("expected empty external ids, got tvdb=%q imdb=%q", meta.TVDBID, meta.IMDbID)
	}
	if meta.HasExternalIDs() {
		t.Error("HasExternalIDs = true, want false")
	}
}

func TestCacheRoundTripPreservesExternalIDs(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	want := &ShowMetadata{
		ProviderID: "1399",
		Provider:   "tmdb",
		ShowName:   "Game of Thrones",
		IMDbID:     "tt0944947",
		TMDBID:     "1399",
		TVDBID:     "121361",
		FetchedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := c.Put("game of thrones", "tmdb", want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := c.Get("game of thrones")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned a miss for a freshly written row")
	}
	if got.TMDBID != want.TMDBID || got.TVDBID != want.TVDBID || got.IMDbID != want.IMDbID {
		t.Errorf("external ids not round-tripped: got %+v", got)
	}
}

func TestCacheStaleFormatVersionIsMiss(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	blob, err := json.Marshal(&ShowMetadata{ShowName: "Legacy Row"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Simulate a row written before external IDs existed.
	_, err = c.db.Exec(
		`INSERT INTO show_metadata (show_key, provider, data, fetched_at, format_version)
		 VALUES (?, ?, ?, ?, ?)`,
		"legacy row", "tvmaze", string(blob), time.Now().Unix(), metaFormatVersion-1,
	)
	if err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	got, err := c.Get("legacy row")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("Get returned %+v for a stale-version row, want a miss", got)
	}
}

func TestCacheMigrateAddsFormatVersionToLegacyTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "curator.db")
	t.Setenv("CURATOR_META_DB", filepath.Join(dir, "curator-meta.db"))

	// First open creates the table at the current shape.
	c1, err := NewCache(path)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	// Drop the column by rebuilding the table in its pre-versioning shape.
	if _, err := c1.db.Exec(`DROP TABLE show_metadata`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := c1.db.Exec(`CREATE TABLE show_metadata (
		show_key TEXT NOT NULL PRIMARY KEY,
		provider TEXT NOT NULL DEFAULT '',
		data TEXT NOT NULL DEFAULT '{}',
		fetched_at INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("recreate legacy table: %v", err)
	}
	if _, err := c1.db.Exec(
		`INSERT INTO show_metadata (show_key, provider, data, fetched_at) VALUES ('x','tvmaze','{}',0)`,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c1.Close()

	c2, err := NewCache(path)
	if err != nil {
		t.Fatalf("reopen NewCache: %v", err)
	}
	defer c2.Close()

	got, err := c2.Get("x")
	if err != nil {
		t.Fatalf("Get after migration: %v", err)
	}
	if got != nil {
		t.Errorf("legacy row should default to format_version 1 and miss, got %+v", got)
	}
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CURATOR_META_DB", filepath.Join(dir, "curator-meta.db"))
	c, err := NewCache(filepath.Join(dir, "curator.db"))
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	return c
}
