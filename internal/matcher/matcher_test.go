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
		{"", "1080P", true},       // unknown quality passes through
		{"1080P", "", true},       // no minimum → always pass
		{"UNKNOWN", "720P", true}, // unrecognised rank → pass
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
