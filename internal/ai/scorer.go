package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/iillmaticc/rss-curator/pkg/models"
)

const scoreSystemPrompt = `You are a torrent preference scorer. Given a candidate torrent and a user's recent approve/reject history,
score how likely the user would want this torrent on a scale of 0.0 to 1.0.
Always respond with a single JSON object. No explanation, no markdown, just raw JSON.
Fields:
  score   (float, 0.0-1.0)   - predicted likelihood of approval
  reason  (string, max 80 chars) - one-line explanation`

type scoreResult struct {
	Score  float64 `json:"score"`
	Reason string  `json:"reason"`
}

// Scorer ranks matched StagedTorrents using the LLM and the user's activity history.
type Scorer struct {
	provider Provider
}

// NewScorer creates a Scorer backed by the given Provider.
func NewScorer(p Provider) *Scorer {
	return &Scorer{provider: p}
}

// ScoreAll attaches AI scores to each staged torrent.
// Torrents that fail scoring retain AIScore=0 and AIReason="".
// Returns the same slice with scores filled in.
func (s *Scorer) ScoreAll(staged []models.StagedTorrent, history []models.Activity) []models.StagedTorrent {
	if !s.provider.Available() {
		return staged
	}

	histCtx := buildHistoryContext(history)
	for i := range staged {
		t := &staged[i]
		t.AIScore, t.AIReason = s.scoreOne(t, histCtx)
	}
	return staged
}

func (s *Scorer) scoreOne(t *models.StagedTorrent, histCtx string) (float64, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	user := fmt.Sprintf(
		"Candidate: %s\nQuality: %s | Codec: %s | Group: %s | Source: %s\nRule match: %s\n\nRecent history:\n%s",
		t.FeedItem.Title,
		t.FeedItem.Quality,
		t.FeedItem.Codec,
		t.FeedItem.ReleaseGroup,
		t.FeedItem.Source,
		t.MatchReason,
		histCtx,
	)

	content, err := s.provider.Complete(ctx, scoreSystemPrompt, user)
	if err != nil {
		return 0, ""
	}

	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result scoreResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return 0, ""
	}

	// Clamp to [0, 1].
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 1 {
		result.Score = 1
	}

	return result.Score, result.Reason
}

// buildHistoryContext produces a compact text summary of recent activity for
// inclusion in the scoring prompt. Capped at the 20 most recent entries.
func buildHistoryContext(history []models.Activity) string {
	if len(history) == 0 {
		return "No history yet."
	}
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	var sb strings.Builder
	for _, h := range history {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", strings.ToUpper(h.Action), h.TorrentTitle))
	}
	return sb.String()
}
