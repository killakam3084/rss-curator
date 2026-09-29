package plex

import "testing"

func TestQualityRank(t *testing.T) {
	tests := map[string]int{
		"480P": 0, "720P": 1, "1080P": 2, "2160P": 3, "4K": 3,
		"1080p": 2, " 2160P ": 3,
		"": -1, "xvid": -1, "sd": -1,
	}
	for in, want := range tests {
		if got := QualityRank(in); got != want {
			t.Errorf("QualityRank(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCompareQualityTiers(t *testing.T) {
	tests := []struct {
		name      string
		candidate Copy
		library   Copy
		want      string
	}{
		{"higher tier is an upgrade", Copy{Quality: "2160P"}, Copy{Quality: "1080P"}, VerdictUpgrade},
		{"lower tier is a duplicate", Copy{Quality: "720P"}, Copy{Quality: "1080P"}, VerdictInLibrary},
		{"same tier is a duplicate", Copy{Quality: "1080P"}, Copy{Quality: "1080P"}, VerdictInLibrary},
		{"4K and 2160P are equivalent", Copy{Quality: "4K"}, Copy{Quality: "2160P"}, VerdictInLibrary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := Compare(tt.candidate, tt.library)
			if got != tt.want {
				t.Errorf("Compare = %q (%s), want %q", got, reason, tt.want)
			}
			if reason == "" {
				t.Error("Compare returned an empty reason")
			}
		})
	}
}

func TestCompareHDRBreaksQualityTies(t *testing.T) {
	tests := []struct {
		name      string
		candidate Copy
		library   Copy
		want      string
	}{
		{
			"dv over none is an upgrade",
			Copy{Quality: "2160P", HDR: []string{"dv"}},
			Copy{Quality: "2160P"},
			VerdictUpgrade,
		},
		{
			"dv over hdr10 is an upgrade",
			Copy{Quality: "2160P", HDR: []string{"dv"}},
			Copy{Quality: "2160P", HDR: []string{"hdr10"}},
			VerdictUpgrade,
		},
		{
			"hdr10plus over hdr10 is an upgrade",
			Copy{Quality: "2160P", HDR: []string{"hdr10plus"}},
			Copy{Quality: "2160P", HDR: []string{"hdr10"}},
			VerdictUpgrade,
		},
		{
			"equal hdr is a duplicate",
			Copy{Quality: "2160P", HDR: []string{"dv", "hdr10plus"}},
			Copy{Quality: "2160P", HDR: []string{"dv", "hdr10plus"}},
			VerdictInLibrary,
		},
		{
			"weaker hdr is a duplicate",
			Copy{Quality: "2160P", HDR: []string{"hdr10"}},
			Copy{Quality: "2160P", HDR: []string{"dv"}},
			VerdictInLibrary,
		},
		{
			"better hdr cannot rescue a lower quality tier",
			Copy{Quality: "1080P", HDR: []string{"dv"}},
			Copy{Quality: "2160P"},
			VerdictInLibrary,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, reason := Compare(tt.candidate, tt.library); got != tt.want {
				t.Errorf("Compare = %q (%s), want %q", got, reason, tt.want)
			}
		})
	}
}

func TestCompareUnknownQualityNeverUpgrades(t *testing.T) {
	// A false "upgrade" is the costlier mistake in a workflow built around
	// bulk-removing duplicates, so unknown tokens must not win.
	tests := []struct {
		name      string
		candidate Copy
		library   Copy
	}{
		{"unknown candidate quality", Copy{Quality: "xvid"}, Copy{Quality: "1080P"}},
		{"unknown library quality", Copy{Quality: "2160P"}, Copy{Quality: "mystery"}},
		{"both unknown", Copy{}, Copy{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, reason := Compare(tt.candidate, tt.library); got != VerdictInLibrary {
				t.Errorf("Compare = %q (%s), want %q", got, reason, VerdictInLibrary)
			}
		})
	}

	// HDR evidence is still allowed to promote an otherwise uncomparable pair,
	// because that signal does not depend on the quality token.
	got, _ := Compare(Copy{Quality: "mystery", HDR: []string{"dv"}}, Copy{Quality: "mystery"})
	if got != VerdictUpgrade {
		t.Errorf("Compare with HDR evidence = %q, want %q", got, VerdictUpgrade)
	}
}

func TestCompareRealWorldMobLandCase(t *testing.T) {
	// The library holds the 2160p DV/HDR10+ REPACK; an incoming 1080p release
	// of the same episode must read as a duplicate, not an upgrade.
	library := Copy{Quality: "2160P", Codec: "x265", HDR: []string{"dv", "hdr10plus"}}

	if got, _ := Compare(Copy{Quality: "1080P", Codec: "x265"}, library); got != VerdictInLibrary {
		t.Errorf("1080p candidate = %q, want %q", got, VerdictInLibrary)
	}
	if got, _ := Compare(Copy{Quality: "2160P", Codec: "x265", HDR: []string{"hdr10"}}, library); got != VerdictInLibrary {
		t.Errorf("2160p HDR10 candidate = %q, want %q", got, VerdictInLibrary)
	}
	if got, _ := Compare(Copy{Quality: "2160P", Codec: "x265", HDR: []string{"dv", "hdr10plus"}}, library); got != VerdictInLibrary {
		t.Errorf("identical candidate = %q, want %q", got, VerdictInLibrary)
	}
}
