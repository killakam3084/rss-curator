package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNewPlexValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PlexConfig
		wantErr string
	}{
		{"empty url", PlexConfig{Token: "t"}, "base URL is required"},
		{"no scheme", PlexConfig{BaseURL: "192.168.1.10:32400", Token: "t"}, "must include an http:// or https:// scheme"},
		{"bad scheme", PlexConfig{BaseURL: "ftp://host:32400", Token: "t"}, "must be http or https"},
		{"no host", PlexConfig{BaseURL: "http://", Token: "t"}, "no host"},
		{"empty token", PlexConfig{BaseURL: "http://host:32400"}, "token is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPlex(tt.cfg)
			if err == nil {
				t.Fatalf("NewPlex(%+v) = nil error, want %q", tt.cfg, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	p, err := NewPlex(PlexConfig{BaseURL: "http://host:32400/", Token: "t"})
	if err != nil {
		t.Fatalf("NewPlex valid config: %v", err)
	}
	if p.BaseURL() != "http://host:32400" {
		t.Errorf("BaseURL() = %q, want trailing slash trimmed", p.BaseURL())
	}
}

func TestPlexIdentity(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Plex-Token")
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q, want application/json", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"abc123","version":"1.40.0"}}`))
	}))
	defer srv.Close()

	p := mustPlex(t, srv.URL)
	id, err := p.Identity(context.Background())
	if err != nil {
		t.Fatalf("Identity: %v", err)
	}
	if id.MachineIdentifier != "abc123" || id.Version != "1.40.0" {
		t.Errorf("Identity = %+v", id)
	}
	if gotToken != "test-token" {
		t.Errorf("X-Plex-Token = %q", gotToken)
	}
}

func TestPlexUnauthorizedMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := mustPlex(t, srv.URL).Identity(context.Background())
	if err == nil {
		t.Fatal("expected an error for 401")
	}
	if !strings.Contains(err.Error(), "X-Plex-Token") {
		t.Errorf("error = %q, want it to mention the token", err)
	}
}

func TestPlexSections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
			{"key":"1","title":"Movies","type":"movie"},
			{"key":"2","title":"TV Shows","type":"show"}
		]}}`))
	}))
	defer srv.Close()

	sections, err := mustPlex(t, srv.URL).Sections(context.Background())
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	if len(sections) != 2 || sections[1].Type != "show" || sections[1].Key != "2" {
		t.Errorf("Sections = %+v", sections)
	}
}

func TestPlexShowsParsesModernGuids(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("type"); got != strconv.Itoa(plexTypeShow) {
			t.Errorf("type = %q, want %d", got, plexTypeShow)
		}
		if r.URL.Query().Get("includeGuids") != "1" {
			t.Error("includeGuids not requested")
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":1,"Metadata":[{
			"ratingKey":"101",
			"title":"Game of Thrones",
			"year":2011,
			"guid":"plex://show/5d9c08544eefaa001f5d6dcb",
			"Guid":[{"id":"imdb://tt0944947"},{"id":"tmdb://1399"},{"id":"tvdb://121361"}]
		}]}}`))
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Shows(context.Background(), "2", onPage)
	})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0]
	if got.IMDbID != "tt0944947" || got.TMDBID != "1399" || got.TVDBID != "121361" {
		t.Errorf("guids not parsed: %+v", got)
	}
	if got.RatingKey != "101" || got.Year != 2011 {
		t.Errorf("item = %+v", got)
	}
}

func TestPlexParsesLegacyAgentGuids(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":1,"Metadata":[{
			"ratingKey":"7",
			"title":"Legacy Show",
			"guid":"com.plexapp.agents.thetvdb://121361/1/1?lang=en"
		}]}}`))
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Shows(context.Background(), "2", onPage)
	})
	if len(items) != 1 || items[0].TVDBID != "121361" {
		t.Errorf("legacy guid not parsed: %+v", items)
	}
}

func TestPlexEpisodesLinkToShowAndPaginate(t *testing.T) {
	const total = plexPageSize + 3
	var requestedOffsets []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
		size, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Size"))
		requestedOffsets = append(requestedOffsets, start)
		if size != plexPageSize {
			t.Errorf("X-Plex-Container-Size = %d, want %d", size, plexPageSize)
		}

		var meta []map[string]any
		for i := start; i < total && i < start+size; i++ {
			meta = append(meta, map[string]any{
				"ratingKey":            fmt.Sprintf("e%d", i),
				"grandparentRatingKey": "101",
				"title":                fmt.Sprintf("Episode %d", i),
				"parentIndex":          3,
				"index":                i,
				"Media": []map[string]any{{
					"videoResolution": "1080",
					"videoCodec":      "hevc",
					"videoProfile":    "main 10",
					"Part":            []map[string]any{{"size": 4096, "file": "/media/ep.mkv"}},
				}},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{"size": len(meta), "Metadata": meta},
		})
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Episodes(context.Background(), "2", onPage)
	})
	if len(items) != total {
		t.Fatalf("got %d episodes, want %d", len(items), total)
	}
	if len(requestedOffsets) != 2 || requestedOffsets[0] != 0 || requestedOffsets[1] != plexPageSize {
		t.Errorf("pagination offsets = %v", requestedOffsets)
	}
	first := items[0]
	if first.GrandparentRatingKey != "101" || first.Season != 3 {
		t.Errorf("episode not linked to show: %+v", first)
	}
	if first.Resolution != "1080" || first.Codec != "hevc" || first.HDR != "hdr10" {
		t.Errorf("media fields = %+v", first)
	}
	if first.FileSize != 4096 || first.FilePath != "/media/ep.mkv" {
		t.Errorf("part fields = %+v", first)
	}
}

func TestPlexListStopsOnExactPageBoundary(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		start, _ := strconv.Atoi(r.Header.Get("X-Plex-Container-Start"))
		var meta []map[string]any
		if start == 0 {
			for i := 0; i < plexPageSize; i++ {
				meta = append(meta, map[string]any{"ratingKey": strconv.Itoa(i), "title": "x"})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{"size": len(meta), "Metadata": meta},
		})
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Movies(context.Background(), "1", onPage)
	})
	if len(items) != plexPageSize {
		t.Fatalf("got %d items, want %d", len(items), plexPageSize)
	}
	if calls != 2 {
		t.Errorf("made %d calls, want 2 (full page then empty page)", calls)
	}
}

func TestPlexListPropagatesCallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":1,"Metadata":[{"ratingKey":"1","title":"x"}]}}`))
	}))
	defer srv.Close()

	sentinel := fmt.Errorf("stop")
	err := mustPlex(t, srv.URL).Movies(context.Background(), "1", func([]PlexItem) error {
		return sentinel
	})
	if err != sentinel {
		t.Errorf("err = %v, want the callback error to propagate", err)
	}
}

func TestNormalizePlexVideoProfile(t *testing.T) {
	tests := map[string]string{
		"":             "",
		"main 10":      "hdr10",
		"main10":       "hdr10",
		"dvhe.05":      "dv",
		"Dolby Vision": "dv",
		"hlg":          "hlg",
		"main":         "",
		"high":         "",
	}
	for in, want := range tests {
		if got := normalizePlexVideoProfile(in); got != want {
			t.Errorf("normalizePlexVideoProfile(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustPlex(t *testing.T, baseURL string) *Plex {
	t.Helper()
	p, err := NewPlex(PlexConfig{BaseURL: baseURL, Token: "test-token"})
	if err != nil {
		t.Fatalf("NewPlex: %v", err)
	}
	return p
}

func collectItems(t *testing.T, call func(func([]PlexItem) error) error) []PlexItem {
	t.Helper()
	var items []PlexItem
	if err := call(func(page []PlexItem) error {
		items = append(items, page...)
		return nil
	}); err != nil {
		t.Fatalf("list call: %v", err)
	}
	return items
}
