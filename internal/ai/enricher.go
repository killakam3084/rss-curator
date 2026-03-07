package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
)

const enrichSystemPrompt = `You are a media title parser. Given a torrent title, extract structured metadata.
Always respond with a single JSON object. No explanation, no markdown, just raw JSON.
Fields:
  show_name  (string)  - human-readable show name, e.g. "The Last of Us"
  season     (int)     - season number, 0 if unknown
  episode    (int)     - episode number, 0 if unknown
  year       (int)     - release year, 0 if unknown`

type enrichResult struct {
	ShowName string `json:"show_name"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	Year     int    `json:"year"`
}

// Enricher uses an LLM to fill in FeedItem fields the regex parser could not determine.
// It is intentionally a fallback - it only fires when ShowName is empty or Season is 0.
type Enricher struct {
	provider Provider
}

// NewEnricher creates an Enricher backed by the given Provider.
func NewEnricher(p Provider) *Enricher {
	return &Enricher{provider: p}
}

// Enrich attempts to fill in missing ShowName / Season / Episode using the LLM.
// It is safe to call on every item - it no-ops if the regex already parsed the
// title successfully, or if the provider is unreachable.
func (e *Enricher) Enrich(item *models.FeedItem) {
	// Only enrich if the regex left ShowName empty or Season unparsed.
	if item.ShowName != "" && item.Season > 0 {
		return
	}

	if !e.provider.Available() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	content, err := e.provider.Complete(ctx, enrichSystemPrompt,
		fmt.Sprintf("Title: %s", item.Title))
	if err != nil {
		return // silent fallback - item unchanged
	}

	// Strip markdown fences if the model wrapped the response.
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result enrichResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return // silent fallback
	}

	if result.ShowName != "" && item.ShowName == "" {
		item.ShowName = result.ShowName
	}
	if result.Season > 0 && item.Season == 0 {
		item.Season = result.Season
	}
	if result.Episode > 0 && item.Episode == 0 {
		item.Episode = result.Episode
	}
}
