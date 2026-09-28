package plex

import "testing"

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already clean", "The Bear", "bear"},
		{"dotted release name", "The.Bear", "bear"},
		{"underscores", "Breaking_Bad", "breaking bad"},
		{"hyphens", "Spider-Man", "spider man"},
		{"colon dropped", "Dune: Part Two", "dune part two"},
		{"trailing year", "Dune Part Two 2024", "dune part two"},
		{"parenthesised year", "Joker (2019)", "joker"},
		{"dotted with year", "Joker.2019", "joker"},
		{"apostrophe folds together", "Don't Look Up", "dont look up"},
		{"ampersand becomes and", "Will & Grace", "will and grace"},
		{"diacritics folded", "Amélie", "amelie"},
		{"leading a", "A Quiet Place", "quiet place"},
		{"leading an", "An Honest Life", "honest life"},
		{"article only stripped once", "The The", "the"},
		{"mixed whitespace collapsed", "  Mr.   Robot  ", "mr robot"},
		{"case folded", "TWIN PEAKS", "twin peaks"},
		{"numeric title preserved", "1899", "1899"},
		{"year-like suffix out of range kept", "Apollo 1234", "apollo 1234"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTitle(tt.in); got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeTitleAgreesAcrossSources(t *testing.T) {
	// Each pair is (release-style name, Plex-style name) that must collapse
	// to the same key for the title fallback tier to work.
	pairs := [][2]string{
		{"The.Bear.S03E01", "The Bear S03E01"},
		{"Dune.Part.Two.2024", "Dune: Part Two"},
		{"Mr.Robot", "Mr. Robot"},
		{"Its.Always.Sunny.in.Philadelphia", "It's Always Sunny in Philadelphia"},
		{"Marvels.Daredevil", "Marvel's Daredevil"},
		{"Amelie", "Amélie"},
	}

	for _, p := range pairs {
		if a, b := NormalizeTitle(p[0]), NormalizeTitle(p[1]); a != b {
			t.Errorf("NormalizeTitle(%q)=%q != NormalizeTitle(%q)=%q", p[0], a, p[1], b)
		}
	}
}
