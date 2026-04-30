package ops

import (
	"testing"

	"github.com/killakam3084/rss-curator/pkg/models"
)

func TestEnrichNameFromMatchReason(t *testing.T) {
	cases := []struct {
		reason  string
		wantN   string
		wantCT  models.ContentType
	}{
		{"matches show: Dark, quality: 1080P, preferred codec: x265", "Dark", models.ContentTypeShow},
		{"matches show: Daredevil Born Again, quality: 2160P", "Daredevil Born Again", models.ContentTypeShow},
		{"matches movie: Oppenheimer, quality: 2160P", "Oppenheimer", models.ContentTypeMovie},
		{"matches movie: Oppenheimer 2023, quality: 2160P", "Oppenheimer 2023", models.ContentTypeMovie},
		{"unrelated reason", "", ""},
	}
	for _, tc := range cases {
		name, ct := enrichNameFromMatchReason(tc.reason)
		if name != tc.wantN {
			t.Errorf("%q: name = %q, want %q", tc.reason, name, tc.wantN)
		}
		if ct != tc.wantCT {
			t.Errorf("%q: ct = %q, want %q", tc.reason, ct, tc.wantCT)
		}
	}
}

func TestEnrichModeCodec(t *testing.T) {
	counts := map[string]int{"x265": 5, "x264": 2}
	if got := enrichModeCodec(counts); got != "x265" {
		t.Errorf("modeCodec = %q, want x265", got)
	}
}

func TestEnrichModeCodec_Tie(t *testing.T) {
	counts := map[string]int{"x265": 3, "x264": 3}
	// Tie broken alphabetically: x264 < x265
	if got := enrichModeCodec(counts); got != "x264" {
		t.Errorf("modeCodec tie = %q, want x264", got)
	}
}

func TestEnrichSortedKeys(t *testing.T) {
	m := map[string]bool{"flux": true, "bloom": true, "saon": true}
	got := enrichSortedKeys(m)
	want := []string{"bloom", "flux", "saon"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEnrichNormalizeName(t *testing.T) {
	if got := enrichNormalizeName("  Dark  "); got != "dark" {
		t.Errorf("normalize = %q, want 'dark'", got)
	}
}
