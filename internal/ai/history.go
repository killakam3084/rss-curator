package ai

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
)

// ShowSummary is a compact, decay-weighted aggregate of historical activity
// for a single show. Designed to be token-efficient when included in prompts.
type ShowSummary struct {
	ShowName      string
	ApproveWeight float64
	RejectWeight  float64
	QueueCount    int
	LastAction    time.Time
	LastTitle     string
	LastQuality   string
}

// BuildShowSummaries builds compact, decay-weighted summaries for the top-N
// history entries. It uses sampleHistory to bound work, so callers pass the
// same size used for sampling elsewhere.
func BuildShowSummaries(history []models.Activity, size int) map[string]*ShowSummary {
	out := make(map[string]*ShowSummary)
	if len(history) == 0 {
		return out
	}

	sampled := sampleHistory(history, size)
	now := time.Now()
	// 7-day half-life gives reasonable recency weighting but keeps older
	// signals visible; configurable later if needed.
	halfLife := 7 * 24 * time.Hour

	for _, h := range sampled {
		show := extractMatchedRule(h.MatchReason)
		if show == "" {
			show = "(unknown)"
		}
		s, ok := out[show]
		if !ok {
			s = &ShowSummary{ShowName: show}
			out[show] = s
		}

		age := now.Sub(h.ActionAt)
		// weight decays exponentially with half-life
		weight := math.Exp(-float64(age) / float64(halfLife))

		switch strings.ToUpper(h.Action) {
		case "APPROVE", "APPROVED":
			s.ApproveWeight += weight
		case "REJECT", "REJECTED":
			s.RejectWeight += weight
		default:
			s.QueueCount++
		}

		if h.ActionAt.After(s.LastAction) {
			s.LastAction = h.ActionAt
			s.LastTitle = h.TorrentTitle
			s.LastQuality = parseQuality(h.MatchReason)
		}
	}

	return out
}

// parseQuality extracts a short quality token from a match reason like
// "matches show: NOVA, quality: 1080P, preferred codec: x265".
func parseQuality(matchReason string) string {
	lower := strings.ToLower(matchReason)
	idx := strings.Index(lower, "quality:")
	if idx < 0 {
		return ""
	}
	rest := matchReason[idx+len("quality:"):]
	rest = strings.TrimSpace(rest)
	// take up to comma or whitespace
	if comma := strings.Index(rest, ","); comma >= 0 {
		return strings.TrimSpace(rest[:comma])
	}
	parts := strings.Fields(rest)
	if len(parts) > 0 {
		return parts[0]
	}
	return rest
}

// formatShortSummary returns a compact one-line representation used in prompts.
func formatShortSummary(s *ShowSummary) string {
	a := int(math.Round(s.ApproveWeight))
	r := int(math.Round(s.RejectWeight))
	last := "-"
	if !s.LastAction.IsZero() {
		last = s.LastAction.Format("2006-01-02")
	}
	q := s.LastQuality
	if q == "" {
		q = "?"
	}
	title := s.LastTitle
	if len(title) > 60 {
		title = title[:57] + "..."
	}
	return fmt.Sprintf("%s: +%d -%d | last:%s | q=%s | ex=%s", s.ShowName, a, r, last, q, title)
}
