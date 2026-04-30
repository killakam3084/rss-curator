package matcher

import (
	"testing"

	"github.com/killakam3084/rss-curator/pkg/models"
)

func TestMatchShowNameEmptyRuleDoesNotMatch(t *testing.T) {
	if matchShowName("The Pitt", "") {
		t.Fatal("expected empty rule name not to match")
	}
}

func TestMatchesShowNameEmptyWatchlistDoesNotMatch(t *testing.T) {
	if matchesShowName("The Pitt", nil) {
		t.Fatal("expected nil watch list not to match")
	}

	if matchesShowName("The Pitt", []string{}) {
		t.Fatal("expected empty watch list not to match")
	}
}

func TestMatchesShowNameIgnoresEmptyEntries(t *testing.T) {
	if matchesShowName("The Pitt", []string{"", "   "}) {
		t.Fatal("expected empty watch list entries not to match")
	}

	if !matchesShowName("The Pitt", []string{"", "The Pitt"}) {
		t.Fatal("expected non-empty watch list entry to match")
	}
}

func TestMatchShowNameWordBoundary(t *testing.T) {
	if matchShowName("Beachfront Bargain Hunt Renovation", "NOVA") {
		t.Fatal("expected NOVA not to match inside Renovation")
	}
}

func TestMatchShowNameDoesNotMatchPrefixOfLongerShowName(t *testing.T) {
	// "Saturday Night Live" rule must NOT fire for "Saturday Night Live UK"
	if matchShowName("Saturday Night Live UK", "Saturday Night Live") {
		t.Fatal("expected 'Saturday Night Live' not to match 'Saturday Night Live UK'")
	}
	// "The Great" rule must NOT fire for "The Great Celebrity Bake Off for SU2C"
	if matchShowName("The Great Celebrity Bake Off for SU2C", "The Great") {
		t.Fatal("expected 'The Great' not to match 'The Great Celebrity Bake Off for SU2C'")
	}
	// Exact matches must still work in both directions
	if !matchShowName("Saturday Night Live", "Saturday Night Live") {
		t.Fatal("expected 'Saturday Night Live' to match itself")
	}
	if !matchShowName("The Great", "The Great") {
		t.Fatal("expected 'The Great' to match itself")
	}
}

func TestLegacyMatchNoConfiguredShowsRejects(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:  nil,
		MinQuality: "1080p",
	})

	ok, reason := m.Match(models.FeedItem{
		ShowName: "The Pitt",
		Quality:  "1080p",
	})
	if ok {
		t.Fatal("expected legacy matcher without configured shows to reject item")
	}
	if reason != "show name not in watch list" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

// ── meetsQuality ──────────────────────────────────────────────────────────────

func TestMeetsQuality(t *testing.T) {
	cases := []struct {
		quality    string
		minQuality string
		want       bool
	}{
		{"1080P", "720P", true},
		{"720P", "1080P", false},
		{"2160P", "1080P", true},
		{"4K", "1080P", true},
		{"720P", "720P", true},
		{"480P", "1080P", false},   // 480p below 1080p minimum → reject
		{"480P", "480P", true},     // exactly meets 480p minimum → accept
		{"480P", "720P", false},    // 480p below 720p minimum → reject
		{"", "1080P", false},       // no recognised quality token → reject when min is set
		{"1080P", "", true},        // no minimum → always pass
		{"UNKNOWN", "720P", false}, // unrecognised quality token → reject when min is set
		{"UNKNOWN", "", true},      // no minimum → always pass even if quality unknown
	}
	for _, tc := range cases {
		got := meetsQuality(tc.quality, tc.minQuality)
		if got != tc.want {
			t.Errorf("meetsQuality(%q, %q) = %v, want %v", tc.quality, tc.minQuality, got, tc.want)
		}
	}
}

// ── isExcludedGroup / isPreferredGroup ────────────────────────────────────────

func TestIsExcludedGroup(t *testing.T) {
	if !isExcludedGroup("YIFY", []string{"yify", "rarbg"}) {
		t.Error("expected YIFY to be excluded (case-insensitive)")
	}
	if isExcludedGroup("NOGROUP", []string{"yify"}) {
		t.Error("NOGROUP should not be excluded")
	}
	if isExcludedGroup("YIFY", nil) {
		t.Error("empty exclude list should never exclude")
	}
}

func TestIsPreferredGroup(t *testing.T) {
	if !isPreferredGroup("NTB", []string{"ntb", "flux"}) {
		t.Error("expected NTB to be preferred (case-insensitive)")
	}
	if isPreferredGroup("YIFY", []string{"ntb"}) {
		t.Error("YIFY should not be preferred")
	}
}

// ── legacy matcher ────────────────────────────────────────────────────────────

func TestLegacyMatchAccepts(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:  []string{"The Pitt"},
		MinQuality: "1080P",
	})
	ok, reason := m.Match(models.FeedItem{ShowName: "The Pitt", Quality: "1080P"})
	if !ok {
		t.Fatalf("expected match, got reason: %q", reason)
	}
}

func TestLegacyMatchQualityTooLow(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:  []string{"The Pitt"},
		MinQuality: "1080P",
	})
	ok, reason := m.Match(models.FeedItem{ShowName: "The Pitt", Quality: "720P"})
	if ok {
		t.Fatal("expected reject for quality too low")
	}
	if !contains(reason, "720P") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestLegacyMatchExcludedGroupRejects(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:     []string{"The Pitt"},
		MinQuality:    "720P",
		ExcludeGroups: []string{"yify"},
	})
	ok, reason := m.Match(models.FeedItem{ShowName: "The Pitt", Quality: "1080P", ReleaseGroup: "YIFY"})
	if ok {
		t.Fatal("expected reject for excluded group")
	}
	if !contains(reason, "excluded") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestLegacyMatchPreferredCodecInReason(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:      []string{"The Pitt"},
		MinQuality:     "720P",
		PreferredCodec: "x265",
	})
	ok, reason := m.Match(models.FeedItem{ShowName: "The Pitt", Quality: "1080P", Codec: "x265"})
	if !ok {
		t.Fatalf("expected match: %q", reason)
	}
	if !contains(reason, "x265") {
		t.Errorf("preferred codec not in reason: %q", reason)
	}
}

func TestLegacyMatchPreferredGroupInReason(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:       []string{"The Pitt"},
		MinQuality:      "720P",
		PreferredGroups: []string{"ntb"},
	})
	ok, reason := m.Match(models.FeedItem{ShowName: "The Pitt", Quality: "1080P", ReleaseGroup: "NTB"})
	if !ok {
		t.Fatalf("expected match: %q", reason)
	}
	if !contains(reason, "NTB") {
		t.Errorf("preferred group not in reason: %q", reason)
	}
}

// ── ShowsConfig matcher ───────────────────────────────────────────────────────

func makeShowsCfg(showName, minQuality string) *models.ShowsConfig {
	return &models.ShowsConfig{
		Shows: []models.ShowRule{
			{Name: showName, MinQuality: minQuality},
		},
		Defaults: models.DefaultRules{MinQuality: "720P"},
	}
}

func TestShowsConfigMatchAccepts(t *testing.T) {
	m := NewMatcher(makeShowsCfg("Breaking Bad", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{ShowName: "Breaking Bad", Quality: "1080P"})
	if !ok {
		t.Fatalf("expected match, got: %q", reason)
	}
}

func TestShowsConfigMatchUnlistedShowRejects(t *testing.T) {
	m := NewMatcher(makeShowsCfg("Breaking Bad", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{ShowName: "Better Call Saul", Quality: "1080P"})
	if ok {
		t.Fatal("expected reject for show not in list")
	}
	if reason != "show not in watch list" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShowsConfigMatchQualityTooLow(t *testing.T) {
	m := NewMatcher(makeShowsCfg("Breaking Bad", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{ShowName: "Breaking Bad", Quality: "720P"})
	if ok {
		t.Fatal("expected reject for quality below show minimum")
	}
	if !contains(reason, "720P") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShowsConfigMatchExcludedGroupRejects(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows: []models.ShowRule{
			{Name: "The Sopranos", MinQuality: "720P", ExcludeGroups: []string{"rarbg"}},
		},
	}
	m := NewMatcher(cfg, nil)
	ok, reason := m.Match(models.FeedItem{ShowName: "The Sopranos", Quality: "1080P", ReleaseGroup: "RARBG"})
	if ok {
		t.Fatal("expected reject for excluded group")
	}
	if !contains(reason, "excluded") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShowsConfigMatchInheritsDefault(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows: []models.ShowRule{
			{Name: "The Wire"}, // no per-show quality
		},
		Defaults: models.DefaultRules{MinQuality: "1080P"},
	}
	m := NewMatcher(cfg, nil)
	ok, _ := m.Match(models.FeedItem{ShowName: "The Wire", Quality: "720P"})
	if ok {
		t.Fatal("expected reject: 720P below default minimum 1080P")
	}
	ok2, _ := m.Match(models.FeedItem{ShowName: "The Wire", Quality: "1080P"})
	if !ok2 {
		t.Fatal("expected accept: 1080P meets default minimum")
	}
}

func TestShowsConfigSetShowsConfigSwitchesToLegacy(t *testing.T) {
	m := NewMatcher(makeShowsCfg("Breaking Bad", "720P"), nil)
	// Passing nil should fall back to legacy; since legacyRules is also nil, it should panic-safe
	m.SetShowsConfig(nil)
	if m.ShowsConfig() != nil {
		t.Error("expected nil ShowsConfig after SetShowsConfig(nil)")
	}
}

func TestShowsConfigSetDefaultsPropagates(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows:    []models.ShowRule{{Name: "Severance"}},
		Defaults: models.DefaultRules{MinQuality: "720P"},
	}
	m := NewMatcher(cfg, nil)
	// Upgrade default to 1080P — now 720P items should be rejected
	m.SetDefaults(models.DefaultRules{MinQuality: "1080P"})
	ok, _ := m.Match(models.FeedItem{ShowName: "Severance", Quality: "720P"})
	if ok {
		t.Fatal("expected reject after raising default quality to 1080P")
	}
}

// ── MatchAll ──────────────────────────────────────────────────────────────────

func TestMatchAll(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{
		ShowNames:  []string{"The Pitt"},
		MinQuality: "1080P",
	})
	items := []models.FeedItem{
		{ShowName: "The Pitt", Quality: "1080P"},
		{ShowName: "The Pitt", Quality: "720P"},          // below min (known quality)
		{ShowName: "Better Call Saul", Quality: "1080P"}, // not in list
	}
	staged := m.MatchAll(items)
	if len(staged) != 1 {
		t.Fatalf("expected 1 staged torrent, got %d", len(staged))
	}
	if staged[0].Status != "pending" {
		t.Errorf("expected status 'pending', got %q", staged[0].Status)
	}
}

func TestMatchAllEmpty(t *testing.T) {
	m := NewMatcher(nil, &models.MatchRule{ShowNames: []string{"X"}})
	if got := m.MatchAll(nil); len(got) != 0 {
		t.Errorf("expected empty slice for nil input, got %d", len(got))
	}
}

// ── movieRuleNameParts ────────────────────────────────────────────────────────

func TestMovieRuleNameParts(t *testing.T) {
	cases := []struct {
		input    string
		wantName string
		wantYear int
	}{
		{"Joker 2019", "Joker", 2019},
		{"The Dark Knight 2008", "The Dark Knight", 2008},
		{"Joker", "Joker", 0},                             // no year in rule
		{"The Thing", "The Thing", 0},                     // no year
		{"The Thing 1982", "The Thing", 1982},             // older film
		{"The Thing 2011", "The Thing", 2011},             // remake
		{"  Dune Part Two 2024  ", "Dune Part Two", 2024}, // trimmed
	}
	for _, tc := range cases {
		gotName, gotYear := movieRuleNameParts(tc.input)
		if gotName != tc.wantName || gotYear != tc.wantYear {
			t.Errorf("movieRuleNameParts(%q) = (%q, %d), want (%q, %d)",
				tc.input, gotName, gotYear, tc.wantName, tc.wantYear)
		}
	}
}

// ── movie matcher ─────────────────────────────────────────────────────────────

func makeMovieCfg(ruleName, minQuality string) *models.ShowsConfig {
	return &models.ShowsConfig{
		Movies: []models.MovieRule{
			{Name: ruleName, MinQuality: minQuality},
		},
		Defaults: models.DefaultRules{MinQuality: "720P"},
	}
}

func TestMovieMatchWithYearInRuleName(t *testing.T) {
	// Rule "Joker 2019" must match a parsed item with ShowName="Joker", ReleaseYear=2019
	m := NewMatcher(makeMovieCfg("Joker 2019", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "Joker",
		ReleaseYear: 2019,
		Quality:     "2160P",
	})
	if !ok {
		t.Fatalf("expected Joker 2019 to match, got: %q", reason)
	}
}

func TestMovieMatchWithYearInRuleNameRejectsWrongYear(t *testing.T) {
	// "The Thing 1982" rule must NOT match The Thing (2011)
	m := NewMatcher(makeMovieCfg("The Thing 1982", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "The Thing",
		ReleaseYear: 2011,
		Quality:     "1080P",
	})
	if ok {
		t.Fatal("expected The Thing 2011 to be rejected by The Thing 1982 rule")
	}
	if reason != "movie not in watch list" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestMovieMatchWithoutYearInRuleMatchesAnyYear(t *testing.T) {
	// Rule "Joker" (no year) should match regardless of release year
	m := NewMatcher(makeMovieCfg("Joker", "1080P"), nil)
	ok, _ := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "Joker",
		ReleaseYear: 2019,
		Quality:     "1080P",
	})
	if !ok {
		t.Fatal("expected year-less rule to match any release year")
	}
}

func TestMovieMatchQualityTooLow(t *testing.T) {
	m := NewMatcher(makeMovieCfg("Joker 2019", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "Joker",
		ReleaseYear: 2019,
		Quality:     "720P",
	})
	if ok {
		t.Fatal("expected reject for quality below minimum")
	}
	if !contains(reason, "720P") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestMovieMatchUnlistedMovieRejects(t *testing.T) {
	m := NewMatcher(makeMovieCfg("Joker 2019", "1080P"), nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "Batman",
		ReleaseYear: 2022,
		Quality:     "1080P",
	})
	if ok {
		t.Fatal("expected reject for movie not in watch list")
	}
	if reason != "movie not in watch list" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

// helper
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestHDRSoftSignalAppended(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows: []models.ShowRule{{
			Name:         "Daredevil Born Again",
			PreferredHDR: []string{"dv"},
		}},
		Defaults: models.DefaultRules{MinQuality: "1080P"},
	}
	m := NewMatcher(cfg, nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeShow,
		ShowName:    "Daredevil Born Again",
		Quality:     "2160P",
		HDR:         []string{"dv", "hdr"},
	})
	if !ok {
		t.Fatalf("expected match, got false: %s", reason)
	}
	if !contains(reason, "hdr: dv") {
		t.Errorf("expected reason to contain 'hdr: dv', got: %q", reason)
	}
}

func TestHDRSoftSignalNoRejection(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows: []models.ShowRule{{
			Name:         "The Boys",
			PreferredHDR: []string{"dv"},
		}},
		Defaults: models.DefaultRules{MinQuality: "1080P"},
	}
	m := NewMatcher(cfg, nil)
	// Item has no HDR — should still match, just no hdr reason
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeShow,
		ShowName:    "The Boys",
		Quality:     "1080P",
		HDR:         nil,
	})
	if !ok {
		t.Fatalf("expected match even without HDR, got false: %s", reason)
	}
	if contains(reason, "hdr:") {
		t.Errorf("expected no hdr reason when item has no HDR, got: %q", reason)
	}
}

func TestHDRSoftSignalFallsBackToDefaults(t *testing.T) {
	cfg := &models.ShowsConfig{
		Shows: []models.ShowRule{{
			Name: "The Pitt",
			// no per-show PreferredHDR
		}},
		Defaults: models.DefaultRules{
			MinQuality:   "1080P",
			PreferredHDR: []string{"dv"},
		},
	}
	m := NewMatcher(cfg, nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeShow,
		ShowName:    "The Pitt",
		Quality:     "2160P",
		HDR:         []string{"dv", "hdr"},
	})
	if !ok {
		t.Fatalf("expected match, got false: %s", reason)
	}
	if !contains(reason, "hdr: dv") {
		t.Errorf("expected reason to contain 'hdr: dv' from defaults, got: %q", reason)
	}
}

func TestHDRSoftSignalMovieMatch(t *testing.T) {
	cfg := &models.ShowsConfig{
		Movies: []models.MovieRule{{
			Name:         "Oppenheimer",
			PreferredHDR: []string{"dv"},
		}},
		Defaults: models.DefaultRules{MinQuality: "1080P"},
	}
	m := NewMatcher(cfg, nil)
	ok, reason := m.Match(models.FeedItem{
		ContentType: models.ContentTypeMovie,
		ShowName:    "Oppenheimer",
		ReleaseYear: 2023,
		Quality:     "2160P",
		HDR:         []string{"dv", "hdr"},
	})
	if !ok {
		t.Fatalf("expected match, got false: %s", reason)
	}
	if !contains(reason, "hdr: dv") {
		t.Errorf("expected reason to contain 'hdr: dv', got: %q", reason)
	}
}
