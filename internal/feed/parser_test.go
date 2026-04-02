package feed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/killakam3084/rss-curator/pkg/models"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		desc string
		want int64
	}{
		{"1.44 GB; TV/Web-DL", 1_546_188_226}, // 1.44 * 1024^3 = 1546188226.56 → truncated
		{"500 MB", 524_288_000},
		{"100 KB", 102_400},
		{"21 GB", 22_548_578_304},
		{"no size here", 0},
		{"", 0},
	}
	for _, tc := range cases {
		got := parseSize(tc.desc)
		diff := got - tc.want
		if diff < -1 || diff > 1 {
			t.Errorf("parseSize(%q) = %d, want ~%d", tc.desc, got, tc.want)
		}
	}
}

func TestParseTitleMetadata_Standard(t *testing.T) {
	item := &models.FeedItem{Title: "Breaking.Bad.S01E02.1080p.WEB-DL.x265-NTB"}
	ParseTitleMetadata(item)

	if item.ShowName != "Breaking Bad" {
		t.Errorf("ShowName = %q, want Breaking Bad", item.ShowName)
	}
	if item.Season != 1 {
		t.Errorf("Season = %d, want 1", item.Season)
	}
	if item.Episode != 2 {
		t.Errorf("Episode = %d, want 2", item.Episode)
	}
	if item.Quality != "1080P" {
		t.Errorf("Quality = %q, want 1080P", item.Quality)
	}
	if item.Codec != "x265" {
		t.Errorf("Codec = %q, want x265", item.Codec)
	}
	if item.Source != "WEB-DL" {
		t.Errorf("Source = %q, want WEB-DL", item.Source)
	}
	if item.ReleaseGroup != "NTB" {
		t.Errorf("ReleaseGroup = %q, want NTB", item.ReleaseGroup)
	}
}

func TestParseTitleMetadata_4K(t *testing.T) {
	item := &models.FeedItem{Title: "The.Wire.S03E05.4K.BluRay.x264-GROUP"}
	ParseTitleMetadata(item)
	if item.Quality != "4K" {
		t.Errorf("Quality = %q, want 4K", item.Quality)
	}
	if item.Source != "BluRay" {
		t.Errorf("Source = %q, want BluRay", item.Source)
	}
	if item.Codec != "x264" {
		t.Errorf("Codec = %q, want x264", item.Codec)
	}
}

func TestParseTitleMetadata_2160p(t *testing.T) {
	item := &models.FeedItem{Title: "Severance.S02E01.2160p.ATVP.WEB-DL.H.265-FLUX"}
	ParseTitleMetadata(item)
	if item.Quality != "2160P" {
		t.Errorf("Quality = %q, want 2160P", item.Quality)
	}
	if item.Codec != "x265" {
		t.Errorf("Codec = %q, want x265 (from H.265), got %q", item.Codec, item.Codec)
	}
	if item.Source != "ATVP" {
		t.Errorf("Source = %q, want ATVP", item.Source)
	}
}

func TestParseTitleMetadata_HEVC(t *testing.T) {
	item := &models.FeedItem{Title: "Show.Name.S01E01.1080p.HDTV.HEVC-GROUP"}
	ParseTitleMetadata(item)
	if item.Codec != "x265" {
		t.Errorf("Codec = %q, want x265 (from HEVC)", item.Codec)
	}
}

func TestParseTitleMetadata_H264(t *testing.T) {
	item := &models.FeedItem{Title: "Show.S01E01.720p.WEBRip.H264-GROUP"}
	ParseTitleMetadata(item)
	if item.Codec != "x264" {
		t.Errorf("Codec = %q, want x264 (from H264)", item.Codec)
	}
}

func TestParseTitleMetadata_SeasonOnly(t *testing.T) {
	item := &models.FeedItem{Title: "The.Sopranos.S04.1080p.BluRay.x265-GROUP"}
	ParseTitleMetadata(item)
	if item.ShowName != "The Sopranos" {
		t.Errorf("ShowName = %q, want The Sopranos", item.ShowName)
	}
	if item.Season != 4 {
		t.Errorf("Season = %d, want 4", item.Season)
	}
	if item.Episode != 0 {
		t.Errorf("Episode = %d, want 0 (season pack)", item.Episode)
	}
}

func TestParseTitleMetadata_ResetsFields(t *testing.T) {
	item := &models.FeedItem{
		Title:        "Show.S01E01.1080p-GROUP",
		ShowName:     "stale",
		Season:       99,
		Quality:      "STALE",
		ReleaseGroup: "OLD",
	}
	ParseTitleMetadata(item)
	if item.ShowName == "stale" {
		t.Error("ShowName was not reset before re-parsing")
	}
	if item.Season == 99 {
		t.Error("Season was not reset before re-parsing")
	}
}

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <item>
      <title>Breaking.Bad.S01E01.1080p.WEB-DL.x265-NTB</title>
      <link>http://example.com/bb.torrent</link>
      <guid>http://example.com/bb-guid-1</guid>
      <pubDate>Mon, 01 Jan 2024 12:00:00 +0000</pubDate>
      <description>1.44 GB; TV/WEB-DL</description>
    </item>
    <item>
      <title>Better.Call.Saul.S06E13.2160p.AMZN.WEB-DL.x265-GROUP</title>
      <link>http://example.com/bcs.torrent</link>
      <guid>http://example.com/bcs-guid-1</guid>
      <pubDate>Tue, 02 Jan 2024 08:00:00 +0000</pubDate>
      <enclosure url="http://example.com/bcs.torrent" length="3221225472" type="application/x-bittorrent"/>
    </item>
  </channel>
</rss>`

func TestParse_ReturnsTwoItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, testRSS)
	}))
	defer srv.Close()

	p := NewParser()
	items, err := p.Parse(srv.URL)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestParse_MetadataExtracted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, testRSS)
	}))
	defer srv.Close()

	p := NewParser()
	items, _ := p.Parse(srv.URL)
	bb := items[0]

	if bb.ShowName != "Breaking Bad" {
		t.Errorf("ShowName = %q, want Breaking Bad", bb.ShowName)
	}
	if bb.Season != 1 || bb.Episode != 1 {
		t.Errorf("Season=%d Episode=%d, want S01E01", bb.Season, bb.Episode)
	}
	if bb.Quality != "1080P" {
		t.Errorf("Quality = %q, want 1080P", bb.Quality)
	}
	if bb.Codec != "x265" {
		t.Errorf("Codec = %q, want x265", bb.Codec)
	}
}

func TestParse_EnclosureSizePreferredOverDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, testRSS)
	}))
	defer srv.Close()

	p := NewParser()
	items, _ := p.Parse(srv.URL)
	bcs := items[1]

	if bcs.Size != 3_221_225_472 {
		t.Errorf("Size = %d, want 3221225472 (from enclosure)", bcs.Size)
	}
}

func TestParse_DescriptionSizeFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, testRSS)
	}))
	defer srv.Close()

	p := NewParser()
	items, _ := p.Parse(srv.URL)
	bb := items[0]

	if bb.Size == 0 {
		t.Error("expected size parsed from description, got 0")
	}
}

func TestParse_Non200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := NewParser()
	_, err := p.Parse(srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestParse_InvalidURLReturnsError(t *testing.T) {
	p := NewParser()
	_, err := p.Parse("http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}

func TestParse_MalformedXMLReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "this is not xml <<<<<")
	}))
	defer srv.Close()

	p := NewParser()
	_, err := p.Parse(srv.URL)
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}
