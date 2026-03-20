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
