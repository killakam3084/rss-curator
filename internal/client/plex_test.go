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
	if first.Resolution != "1080P" || first.Codec != "x265" {
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

func TestNormalizePlexResolution(t *testing.T) {
	tests := map[string]string{
		"4k":    "2160P",
		"2160":  "2160P",
		"1080":  "1080P",
		"720":   "720P",
		"480":   "480P",
		"sd":    "480P",
		"":      "",
		"weird": "WEIRD",
	}
	for in, want := range tests {
		if got := normalizePlexResolution(in); got != want {
			t.Errorf("normalizePlexResolution(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePlexCodec(t *testing.T) {
	tests := map[string]string{
		"hevc": "x265",
		"h264": "x264",
		"avc":  "x264",
		"":     "",
		"vp9":  "vp9",
	}
	for in, want := range tests {
		if got := normalizePlexCodec(in); got != want {
			t.Errorf("normalizePlexCodec(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractPlexHDRIgnoresMain10Profile(t *testing.T) {
	// "main 10" only means 10-bit HEVC — it is not evidence of HDR.
	media := plexMedia{
		Part: []plexPart{{Stream: []plexStream{{
			StreamType:   1,
			DisplayTitle: "1080p",
			ColorTrc:     "bt709",
		}}}},
	}
	if got := extractPlexHDR(media); got != nil {
		t.Errorf("extractPlexHDR = %v, want nil for an SDR stream", got)
	}
}

func TestExtractPlexHDRFromStreamFlags(t *testing.T) {
	media := plexMedia{
		Part: []plexPart{{Stream: []plexStream{
			{StreamType: 2, DisplayTitle: "English (EAC3 5.1 + Atmos)"},
			{
				StreamType:       1,
				DisplayTitle:     "4K DoVi/HDR10+",
				DOVIPresent:      true,
				HDR10PlusPresent: true,
				ColorTrc:         "smpte2084",
			},
		}}},
	}
	got := extractPlexHDR(media)
	want := []string{"dv", "hdr10plus"}
	if len(got) != len(want) {
		t.Fatalf("extractPlexHDR = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("extractPlexHDR = %v, want %v", got, want)
		}
	}
}

func TestExtractPlexHDRFromColorTrcAlone(t *testing.T) {
	media := plexMedia{
		Part: []plexPart{{Stream: []plexStream{{StreamType: 1, ColorTrc: "smpte2084"}}}},
	}
	got := extractPlexHDR(media)
	if len(got) != 1 || got[0] != "hdr10" {
		t.Errorf("extractPlexHDR = %v, want [hdr10]", got)
	}

	hlg := plexMedia{
		Part: []plexPart{{Stream: []plexStream{{StreamType: 1, ColorTrc: "arib-std-b67"}}}},
	}
	if got := extractPlexHDR(hlg); len(got) != 1 || got[0] != "hlg" {
		t.Errorf("extractPlexHDR = %v, want [hlg]", got)
	}
}

func TestSelectMediaSkipsOptimizedVersions(t *testing.T) {
	// Mirrors a real library entry: a 4K original plus a Plex-generated
	// "Tablet-1080p-Low" transcode. Reading the transcode would make a 2160p
	// release look like a duplicate of a 1080p copy the user never downloaded.
	media := []plexMedia{
		{VideoResolution: "4k", VideoCodec: "hevc", Duration: 2934556, Title: "Original"},
		{VideoResolution: "1080", VideoCodec: "h264", Duration: 2934580, ProxyType: 42, Title: "Tablet-1080p-Low"},
	}
	got := selectMedia(media)
	if got == nil || got.Title != "Original" {
		t.Fatalf("selectMedia = %+v, want the Original entry", got)
	}

	// Order must not matter.
	reversed := []plexMedia{media[1], media[0]}
	if got := selectMedia(reversed); got == nil || got.Title != "Original" {
		t.Fatalf("selectMedia(reversed) = %+v, want the Original entry", got)
	}
}

func TestSelectMediaSkipsSampleFiles(t *testing.T) {
	// Verbatim from a live library: the release ships a Sample.mkv alongside
	// the feature. Both are 4k and neither is a Plex proxy, and the sample has
	// the HIGHER bitrate — which is why bitrate must not be the tie-breaker.
	media := []plexMedia{
		{
			VideoResolution: "4k", VideoCodec: "hevc", Duration: 8447333, Title: "feature",
			Part: []plexPart{{Size: 26999399756, File: "/media/movies/A.Complete.Unknown.2025.mkv"}},
		},
		{
			VideoResolution: "4k", VideoCodec: "hevc", Duration: 102000, Title: "sample",
			Part: []plexPart{{Size: 371383739, File: "/media/movies/Sample.mkv"}},
		},
	}

	got := selectMedia(media)
	if got == nil || got.Title != "feature" {
		t.Fatalf("selectMedia = %+v, want the feature not the sample", got)
	}
	if got := selectMedia([]plexMedia{media[1], media[0]}); got.Title != "feature" {
		t.Fatalf("selectMedia(reversed) = %q, want feature", got.Title)
	}
}

func TestSelectMediaFallsBackWhenAllAreProxies(t *testing.T) {
	media := []plexMedia{{VideoResolution: "1080", ProxyType: 42, Title: "only-proxy"}}
	if got := selectMedia(media); got == nil || got.Title != "only-proxy" {
		t.Errorf("selectMedia = %+v, want the proxy as a fallback", got)
	}
	if got := selectMedia(nil); got != nil {
		t.Errorf("selectMedia(nil) = %+v, want nil", got)
	}
}

func TestIsOptimizedVersionMarkers(t *testing.T) {
	// Servers are inconsistent about which marker they set, so each one alone
	// must be enough to disqualify a transcode.
	tests := []struct {
		name  string
		media plexMedia
		want  bool
	}{
		{"source file", plexMedia{Part: []plexPart{{File: "/media/movies/Film.mkv"}}}, false},
		{"proxyType only", plexMedia{ProxyType: 42}, true},
		{"target only", plexMedia{Target: "Tablet-1080p-Low"}, true},
		{"plex versions path only", plexMedia{
			Part: []plexPart{{File: "/media/movies/Film/Plex Versions/Optimized for Mobile/Film.mp4"}},
		}, true},
		{"blank target is not a marker", plexMedia{Target: "  "}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isOptimizedVersion(tt.media); got != tt.want {
				t.Errorf("isOptimizedVersion = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectMediaSkipsHigherResolutionOptimizedVersion(t *testing.T) {
	// An optimized version must lose even when it somehow outranks the source
	// on resolution, because it is not what the user actually owns.
	media := []plexMedia{
		{VideoResolution: "1080", Duration: 9_000_000, Title: "source",
			Part: []plexPart{{File: "/media/tv/Show S01E01.mkv"}}},
		{VideoResolution: "4k", Duration: 9_000_000, Title: "optimized", Target: "Apple TV",
			Part: []plexPart{{File: "/media/tv/Show S01E01/Plex Versions/Apple TV/S01E01.mp4"}}},
	}
	if got := selectMedia(media); got.Title != "source" {
		t.Errorf("selectMedia = %q, want source", got.Title)
	}
}

func TestSelectMediaPrefersHigherResolutionThenDurationThenSize(t *testing.T) {
	resolutionWins := []plexMedia{
		{VideoResolution: "1080", Duration: 9_000_000, Title: "hd"},
		{VideoResolution: "4k", Duration: 100, Title: "uhd"},
	}
	if got := selectMedia(resolutionWins); got.Title != "uhd" {
		t.Errorf("selectMedia = %q, want uhd", got.Title)
	}

	durationWins := []plexMedia{
		{VideoResolution: "1080", Duration: 100, Title: "short"},
		{VideoResolution: "1080", Duration: 9_000_000, Title: "full"},
	}
	if got := selectMedia(durationWins); got.Title != "full" {
		t.Errorf("selectMedia = %q, want full", got.Title)
	}

	// Equal duration falls through to total part size.
	sizeWins := []plexMedia{
		{VideoResolution: "1080", Duration: 100, Title: "small", Part: []plexPart{{Size: 1}}},
		{VideoResolution: "1080", Duration: 100, Title: "big", Part: []plexPart{{Size: 2}}},
	}
	if got := selectMedia(sizeWins); got.Title != "big" {
		t.Errorf("selectMedia = %q, want big", got.Title)
	}
}

// TestPlexEpisodeRealPayload exercises a verbatim capture from a live Plex
// server: a 4K DV/HDR10+ episode that also carries an optimized 1080p version.
func TestPlexEpisodeRealPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mobLandEpisodeJSON))
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Episodes(context.Background(), "1", onPage)
	})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0]

	if got.RatingKey != "24256" || got.GrandparentRatingKey != "17784" {
		t.Errorf("keys = %+v", got)
	}
	if got.Season != 2 || got.Episode != 1 {
		t.Errorf("season/episode = %d/%d, want 2/1", got.Season, got.Episode)
	}
	if got.IMDbID != "tt37427409" || got.TMDBID != "7492637" || got.TVDBID != "11542638" {
		t.Errorf("guids = %+v", got)
	}
	if got.Resolution != "2160P" {
		t.Errorf("Resolution = %q, want 2160P (Plex reports \"4k\")", got.Resolution)
	}
	if got.Codec != "x265" {
		t.Errorf("Codec = %q, want x265", got.Codec)
	}
	if len(got.HDR) != 2 || got.HDR[0] != "dv" || got.HDR[1] != "hdr10plus" {
		t.Errorf("HDR = %v, want [dv hdr10plus]", got.HDR)
	}
	if got.FileSize != 9351276799 {
		t.Errorf("FileSize = %d, want the original file size not the transcode", got.FileSize)
	}
	if strings.Contains(got.FilePath, "Plex Versions") {
		t.Errorf("FilePath = %q, picked the optimized version", got.FilePath)
	}
}

const mobLandEpisodeJSON = `{"MediaContainer":{"size":1,"Metadata":[{
  "ratingKey":"24256",
  "grandparentRatingKey":"17784",
  "type":"episode",
  "title":"I Wanna Be Your Dog",
  "grandparentTitle":"MobLand",
  "index":1,
  "parentIndex":2,
  "year":2026,
  "guid":"plex://episode/695c50015e8e6d388f11d441",
  "Media":[
    {
      "id":27570,"bitrate":25490,"videoCodec":"hevc","videoResolution":"4k",
      "duration":2934556,"container":"mkv","videoProfile":"main 10","title":"Original",
      "Part":[{
        "id":27624,
        "file":"/mnt/cell_block_d/media/video/television/MobLand S02E01/MobLand S02E01.mkv",
        "size":9351276799,"videoProfile":"main 10",
        "Stream":[
          {"id":91874,"streamType":1,"codec":"hevc","DOVIPresent":true,"DOVIProfile":8,
           "HDR10PlusPresent":true,"bitDepth":10,"colorPrimaries":"bt2020","colorTrc":"smpte2084",
           "profile":"main 10","displayTitle":"4K DoVi/HDR10+",
           "extendedDisplayTitle":"4K DoVi/HDR10+ (HEVC Main 10)"},
          {"id":91875,"streamType":2,"codec":"eac3","displayTitle":"English (UK) (EAC3 5.1 + Atmos)"}
        ]
      }]
    },
    {
      "id":27573,"bitrate":5115,"videoCodec":"h264","videoResolution":"1080",
      "duration":2934580,"container":"mp4","proxyType":42,"target":"Tablet-1080p-Low",
      "videoProfile":"constrained baseline","title":"Tablet-1080p-Low",
      "Part":[{
        "id":27627,
        "file":"/mnt/cell_block_d/media/video/television/MobLand S02E01/Plex Versions/Tablet-1080p-Low 2521/MobLand/S02E01.mp4",
        "size":1877987382,
        "Stream":[
          {"id":91906,"streamType":1,"codec":"h264","bitDepth":8,"colorTrc":"bt709",
           "profile":"constrained baseline","displayTitle":"1080p",
           "extendedDisplayTitle":"1080p (H.264 Constrained Baseline)"}
        ]
      }]
    }
  ],
  "Guid":[{"id":"imdb://tt37427409"},{"id":"tmdb://7492637"},{"id":"tvdb://11542638"}]
}]}}`

func TestPlexItemFetchesDetail(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(mobLandEpisodeJSON))
	}))
	defer srv.Close()

	item, err := mustPlex(t, srv.URL).Item(context.Background(), "24256")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if item == nil {
		t.Fatal("Item returned nil")
	}
	if gotPath != "/library/metadata/24256" {
		t.Errorf("path = %q", gotPath)
	}
	if len(item.HDR) != 2 || item.HDR[0] != "dv" {
		t.Errorf("HDR = %v, want [dv hdr10plus]", item.HDR)
	}
}

func TestPlexItemNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	}))
	defer srv.Close()

	item, err := mustPlex(t, srv.URL).Item(context.Background(), "999")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if item != nil {
		t.Errorf("Item = %+v, want nil for an empty container", item)
	}
}

func TestPlexMovieRealPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(amadeusMovieJSON))
	}))
	defer srv.Close()

	items := collectItems(t, func(onPage func([]PlexItem) error) error {
		return mustPlex(t, srv.URL).Movies(context.Background(), "4", onPage)
	})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 — Extras and Related must not be parsed as library entries", len(items))
	}
	got := items[0]

	if got.RatingKey != "17997" || got.Title != "Amadeus" || got.Year != 1984 {
		t.Errorf("item = %+v", got)
	}
	if got.IMDbID != "tt0086879" || got.TMDBID != "279" || got.TVDBID != "3982" {
		t.Errorf("guids = %+v", got)
	}
	if got.Resolution != "1080P" {
		t.Errorf("Resolution = %q, want 1080P", got.Resolution)
	}
	if got.Codec != "vc1" {
		t.Errorf("Codec = %q, want vc1 passed through unchanged", got.Codec)
	}
	if len(got.HDR) != 0 {
		t.Errorf("HDR = %v, want empty for a bt709 SDR remux", got.HDR)
	}
	if got.FileSize != 25890801371 {
		t.Errorf("FileSize = %d", got.FileSize)
	}
}

// amadeusMovieJSON is a live-server movie entry: an SDR VC-1 remux that also
// carries trailer Extras and a Related hub holding a different film. Only the
// top-level entry may be read as a library item.
const amadeusMovieJSON = `{"MediaContainer":{"size":1,"Metadata":[{
  "ratingKey":"17997",
  "type":"movie",
  "title":"Amadeus",
  "year":1984,
  "guid":"plex://movie/5d776825151a60001f24a5d2",
  "Media":[{
    "id":17100,"duration":10825696,"bitrate":19133,"videoCodec":"vc1",
    "videoResolution":"1080","container":"mkv","videoProfile":"advanced",
    "Part":[{
      "id":17154,
      "file":"/mnt/cell_block_d/media/video/movies/Amadeus 1984 Director's Cut 1080p Bluray Remux VC-1.mkv",
      "size":25890801371,
      "Stream":[
        {"id":39694,"streamType":1,"codec":"vc1","bitDepth":8,"colorPrimaries":"bt709",
         "colorSpace":"bt709","colorTrc":"bt709","profile":"advanced",
         "displayTitle":"1080p","extendedDisplayTitle":"1080p (VC1)"},
        {"id":39695,"streamType":2,"codec":"truehd","displayTitle":"English (TRUEHD 5.1)"}
      ]
    }]
  }],
  "Guid":[{"id":"imdb://tt0086879"},{"id":"tmdb://279"},{"id":"tvdb://3982"}],
  "Extras":{"size":1,"Metadata":[{
    "ratingKey":"18000","type":"clip","title":"Amadeus (4K Trailer)","subtype":"trailer",
    "Media":[{"id":17104,"duration":119000,"videoCodec":"h264","videoResolution":"1080",
      "Part":[{"id":17158,"size":0}]}]
  }]},
  "Related":{"Hub":[{"hubIdentifier":"movie.similar","Metadata":[{
    "ratingKey":"16517","type":"movie","title":"A Complete Unknown","year":2024,
    "Guid":[{"id":"imdb://tt11563598"}],
    "Media":[{"id":14599,"duration":8447333,"videoCodec":"hevc","videoResolution":"4k",
      "Part":[{"id":14652,"size":26999399756}]}]
  }]}]}
}]}}`

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
