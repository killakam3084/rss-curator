package suggester

import (
	"testing"

	"github.com/killakam3084/rss-curator/pkg/models"
)

// ── qualityStr ────────────────────────────────────────────────────────────────

func TestQualityStr(t *testing.T) {
	if got := qualityStr(""); got != "not specified" {
		t.Errorf("empty: got %q", got)
	}
	if got := qualityStr("1080p"); got != "1080p" {
		t.Errorf("1080p: got %q", got)
	}
}

// ── sanitizeQuality ───────────────────────────────────────────────────────────

func TestSanitizeQuality(t *testing.T) {
	cases := []struct {
		raw, def, want string
	}{
		{"1080P", "", "1080p"},
		{"2160p BluRay", "", "2160p"},
		{"4K HDR", "", "4k"},
		{"720p", "1080p", "720p"}, // raw wins
		{"garbage", "1080p", "1080p"},
		{"", "720p", "720p"},
		{"", "", "1080p"}, // hard fallback
	}
	for _, c := range cases {
		if got := sanitizeQuality(c.raw, c.def); got != c.want {
			t.Errorf("sanitizeQuality(%q, %q) = %q, want %q", c.raw, c.def, got, c.want)
		}
	}
}

// ── sanitizeCodec ─────────────────────────────────────────────────────────────

func TestSanitizeCodec(t *testing.T) {
	cases := []struct {
		raw, def, want string
	}{
		{"x265", "", "x265"},
		{"H.265 HEVC", "", "hevc"},
		{"X264", "", "x264"},
		{"AV1", "x265", "av1"}, // raw wins
		{"unknown", "x265", "x265"},
		{"unknown", "garbage", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := sanitizeCodec(c.raw, c.def); got != c.want {
			t.Errorf("sanitizeCodec(%q, %q) = %q, want %q", c.raw, c.def, got, c.want)
		}
	}
}

// ── normalizeName ─────────────────────────────────────────────────────────────

func TestNormalizeName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Succession", "succession"},
		{"Daredevil: Born Again", "daredevilbornagain"},
		{"Mr. Robot", "mrrobot"},
		{"The Bear", "thebear"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeName(c.in); got != c.want {
			t.Errorf("normalizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── extractShowName ───────────────────────────────────────────────────────────

func TestExtractShowName(t *testing.T) {
	cases := []struct {
		reason, want string
	}{
		{"matches show: NOVA, quality: 1080P", "NOVA"},
		{"matches show: Severance", "Severance"},
		{"quality: 1080P", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := extractShowName(c.reason); got != c.want {
			t.Errorf("extractShowName(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

// ── buildHistoryBlock ─────────────────────────────────────────────────────────

func TestBuildHistoryBlock_Empty(t *testing.T) {
	got := buildHistoryBlock(nil, 5)
	if got == "" {
		t.Error("expected non-empty fallback for empty history")
	}
}

func TestBuildHistoryBlock_Dedup(t *testing.T) {
	history := []models.Activity{
		{TorrentTitle: "Show A S01E01", MatchReason: "matches show: Show A"},
		{TorrentTitle: "Show A S01E02", MatchReason: "matches show: Show A"}, // duplicate name
		{TorrentTitle: "Show B S01E01", MatchReason: "matches show: Show B"},
	}
	got := buildHistoryBlock(history, 10)
	// "Show A" should appear only once.
	count := 0
	for i := 0; i < len(got)-len("Show A")+1; i++ {
		if got[i:i+len("Show A")] == "Show A" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected Show A exactly once, found %d times in: %q", count, got)
	}
}

func TestBuildHistoryBlock_MaxEntries(t *testing.T) {
	history := make([]models.Activity, 20)
	for i := range history {
		history[i] = models.Activity{TorrentTitle: ""}
		// All unique names via MatchReason
		history[i].MatchReason = ""
	}
	// All blank match reasons → falls back to TorrentTitle which is also blank → single dedup entry
	got := buildHistoryBlock(history, 3)
	if got == "" {
		t.Error("expected non-empty output")
	}
}

// ── parseResponse ─────────────────────────────────────────────────────────────

func newMinimalSuggester() *Suggester {
	return &Suggester{}
}

func TestParseResponse_BareArray(t *testing.T) {
	sg := newMinimalSuggester()
	raw := `[{"show_name":"Succession","reason":"great drama","quality":"1080p","codec":"x265","content_type":"show"}]`
	out, err := sg.parseResponse(raw, "1080p", "x265")
	if err != nil {
		t.Fatalf("parseResponse: %v", err)
	}
	if len(out) != 1 || out[0].ShowName != "Succession" {
		t.Errorf("unexpected output: %+v", out)
	}
	if out[0].ContentType != models.ContentTypeShow {
		t.Errorf("expected show content type")
	}
}

func TestParseResponse_WrappedObject(t *testing.T) {
	sg := newMinimalSuggester()
	raw := `{"suggestions":[{"show_name":"Dune Part Two","reason":"epic","quality":"2160p","codec":"","content_type":"movie"}]}`
	out, err := sg.parseResponse(raw, "1080p", "")
	if err != nil {
		t.Fatalf("parseResponse wrapped: %v", err)
	}
	if len(out) != 1 || out[0].ContentType != models.ContentTypeMovie {
		t.Errorf("unexpected: %+v", out)
	}
}

func TestParseResponse_MarkdownFence(t *testing.T) {
	sg := newMinimalSuggester()
	raw := "```json\n{\"suggestions\":[{\"show_name\":\"Andor\",\"reason\":\"star wars\",\"quality\":\"1080p\",\"codec\":\"x265\",\"content_type\":\"show\"}]}\n```"
	out, err := sg.parseResponse(raw, "1080p", "x265")
	if err != nil {
		t.Fatalf("parseResponse markdown: %v", err)
	}
	if len(out) != 1 || out[0].ShowName != "Andor" {
		t.Errorf("unexpected: %+v", out)
	}
}

func TestParseResponse_TitleFallback(t *testing.T) {
	sg := newMinimalSuggester()
	// Model emits "title" instead of "show_name"
	raw := `{"suggestions":[{"title":"The Bear","reason":"comedy","quality":"1080p","codec":"","content_type":"show"}]}`
	out, err := sg.parseResponse(raw, "1080p", "")
	if err != nil {
		t.Fatalf("parseResponse title fallback: %v", err)
	}
	if len(out) != 1 || out[0].ShowName != "The Bear" {
		t.Errorf("unexpected: %+v", out)
	}
}

func TestParseResponse_InvalidJSON(t *testing.T) {
	sg := newMinimalSuggester()
	_, err := sg.parseResponse("not json at all %%", "1080p", "x265")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseResponse_EmptySuggestions(t *testing.T) {
	sg := newMinimalSuggester()
	raw := `{"suggestions":[]}`
	out, err := sg.parseResponse(raw, "1080p", "x265")
	if err != nil {
		t.Fatalf("parseResponse empty: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty, got %d", len(out))
	}
}

func TestParseResponse_SkipsBlankNames(t *testing.T) {
	sg := newMinimalSuggester()
	raw := `{"suggestions":[{"show_name":"","title":"","reason":"bad","content_type":"show"},{"show_name":"Good Show","reason":"ok","content_type":"show"}]}`
	out, err := sg.parseResponse(raw, "1080p", "")
	if err != nil {
		t.Fatalf(": %v", err)
	}
	if len(out) != 1 || out[0].ShowName != "Good Show" {
		t.Errorf("expected 1 with Good Show, got %+v", out)
	}
}
