package suggester

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
)

const systemPrompt = `You are a TV/movie watchlist assistant. You help users discover new shows to add to their watchlist.

You will be given:
1. The user's current watchlist with TV metadata (genres, network, status, premiere year) where available.
2. Recent show release titles they have explicitly approved — these are strong positive signals about their taste.
3. Their inferred quality/codec preferences from approval history.

Your task: recommend TV shows or movies the user does NOT already have on their watchlist that they are likely to enjoy, based on their demonstrated tastes.

Rules:
- NEVER suggest a show that is already in the user's watchlist (the watchlist is authoritative).
- Use the correct full title (title case, correct punctuation, correct article placement).
- Prefer currently-airing or critically-acclaimed ended shows over cancelled/obscure titles.
- Match genre profile as closely as possible — look for patterns across the watchlist.
- Include the user's typical quality and codec values on every suggestion.

Respond with a single JSON object. No markdown, no explanation outside the JSON, just raw JSON.
Format:
{
  "suggestions": [
    {
      "show_name": "Show Name",
      "reason": "one-line reason grounded in watchlist patterns",
      "quality": "quality string matching user preference e.g. 1080p",
      "codec": "codec string matching user preference e.g. x264"
    }
  ]
}`

// Suggestion is a single LLM-proposed show that the user might want to add.
type Suggestion struct {
	ShowName      string          `json:"show_name"`
	Reason        string          `json:"reason"`
	SuggestedRule models.ShowRule `json:"suggested_rule"`
}

// llmSuggestion is the raw JSON shape the LLM writes per suggestion.
type llmSuggestion struct {
	ShowName string `json:"show_name"`
	Reason   string `json:"reason"`
	Quality  string `json:"quality"`
	Codec    string `json:"codec"`
}

type llmResponse struct {
	Suggestions []llmSuggestion `json:"suggestions"`
}

// Suggester produces watchlist recommendations by combining:
//   - The current shows.json watch list (taste profile)
//   - Cached TV metadata (genres/network/status — RAG context for the LLM)
//   - Recent approval history (recency/depth signals)
//   - Inferred quality/codec defaults from historical approvals
type Suggester struct {
	store      storage.Store
	provider   ai.Provider
	matcher    *matcher.Matcher
	metaLookup *metadata.Lookup
	timeoutSec int
}

// New creates a Suggester. All fields are required; nil values are tolerated
// but will cause Suggest to return an empty slice rather than panic.
func New(store storage.Store, provider ai.Provider, m *matcher.Matcher, lu *metadata.Lookup) *Suggester {
	return &Suggester{
		store:      store,
		provider:   provider,
		matcher:    m,
		metaLookup: lu,
		timeoutSec: 90,
	}
}

// Available reports whether the underlying AI provider is reachable.
func (sg *Suggester) Available() bool {
	if sg.provider == nil {
		return false
	}
	return sg.provider.Available()
}

// ShowsCount returns the number of shows currently in the watchlist, or 0 if
// the matcher or its config is nil.
func (sg *Suggester) ShowsCount() int {
	if sg.matcher == nil {
		return 0
	}
	cfg := sg.matcher.ShowsConfig()
	if cfg == nil {
		return 0
	}
	return len(cfg.Shows)
}

// Suggest calls the LLM to produce up to limit show recommendations.
// Returns an empty slice (not an error) when the provider is unavailable,
// the watchlist is empty, or no parseable suggestions are returned.
func (sg *Suggester) Suggest(ctx context.Context, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		limit = 5
	}

	// Guard: nothing to work with.
	if sg.provider == nil || !sg.provider.Available() {
		return []Suggestion{}, nil
	}
	cfg := sg.matcher.ShowsConfig()
	if cfg == nil || len(cfg.Shows) == 0 {
		return []Suggestion{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(sg.timeoutSec)*time.Second)
	defer cancel()

	// Build enriched watchlist block.
	watchlistBlock := sg.buildWatchlistBlock(ctx, cfg.Shows)

	// Fetch recent approval history.
	history, err := sg.store.GetActivity(60, 0, "approve")
	if err != nil {
		history = nil // non-fatal — proceed without history
	}
	historyBlock := buildHistoryBlock(history, 30)

	// Infer quality/codec defaults from DB.
	defaultQuality, defaultCodec, _ := sg.store.GetApprovalQualityProfile()

	userPrompt := fmt.Sprintf(
		"Current watchlist (%d shows):\n%s\nRecent approvals (taste signals):\n%s\nInferred quality preferences:\n- Quality: %s\n- Codec: %s\n\nPlease suggest up to %d shows I might want to add. Only suggest shows NOT in my current watchlist.",
		len(cfg.Shows),
		watchlistBlock,
		historyBlock,
		qualityStr(defaultQuality),
		qualityStr(defaultCodec),
		limit,
	)

	raw, err := sg.provider.Complete(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("suggester: LLM call failed: %w", err)
	}

	return sg.parseResponse(raw, defaultQuality, defaultCodec)
}

// buildWatchlistBlock creates the enriched watchlist text block for the prompt.
func (sg *Suggester) buildWatchlistBlock(ctx context.Context, shows []models.ShowRule) string {
	var sb strings.Builder
	for i, show := range shows {
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, show.Name))

		// Attempt metadata enrichment (cache-only — no external quota).
		if sg.metaLookup != nil {
			if meta := sg.metaLookup.Resolve(ctx, show.Name); meta != nil {
				var parts []string
				if len(meta.Genres) > 0 {
					parts = append(parts, "Genres: "+strings.Join(meta.Genres, ", "))
				}
				if meta.Network != "" {
					parts = append(parts, "Network: "+meta.Network)
				}
				if meta.Status != "" {
					parts = append(parts, "Status: "+meta.Status)
				}
				if meta.PremiereYear > 0 {
					parts = append(parts, fmt.Sprintf("Since: %d", meta.PremiereYear))
				}
				if meta.Overview != "" {
					overview := meta.Overview
					if dot := strings.Index(overview, ". "); dot >= 0 && dot < 120 {
						overview = overview[:dot+1]
					} else if len(overview) > 120 {
						overview = overview[:120] + "…"
					}
					parts = append(parts, "About: "+overview)
				}
				if len(parts) > 0 {
					sb.WriteString(" (" + strings.Join(parts, " | ") + ")")
				}
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildHistoryBlock summarises recent approvals as a concise text block.
func buildHistoryBlock(history []models.Activity, maxEntries int) string {
	if len(history) == 0 {
		return "No recent approvals on record.\n"
	}
	if len(history) > maxEntries {
		history = history[:maxEntries]
	}
	var sb strings.Builder
	for _, a := range history {
		sb.WriteString(fmt.Sprintf("- %s (approved %s)\n",
			a.TorrentTitle,
			a.ActionAt.Format("Jan 02 2006"),
		))
	}
	return sb.String()
}

// parseResponse strips markdown fencing, unmarshals the LLM JSON, and maps
// each llmSuggestion into a Suggestion with a ready-to-use SuggestedRule.
// Falls back to defaults for quality/codec when the LLM omits them.
func (sg *Suggester) parseResponse(raw, defaultQuality, defaultCodec string) ([]Suggestion, error) {
	content := strings.TrimSpace(raw)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var resp llmResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("suggester: could not parse LLM response: %w", err)
	}

	out := make([]Suggestion, 0, len(resp.Suggestions))
	for _, s := range resp.Suggestions {
		if s.ShowName == "" {
			continue
		}
		quality := s.Quality
		if quality == "" {
			quality = defaultQuality
		}
		codec := s.Codec
		if codec == "" {
			codec = defaultCodec
		}
		out = append(out, Suggestion{
			ShowName: s.ShowName,
			Reason:   s.Reason,
			SuggestedRule: models.ShowRule{
				Name:           s.ShowName,
				MinQuality:     quality,
				PreferredCodec: codec,
			},
		})
	}
	return out, nil
}

func qualityStr(s string) string {
	if s == "" {
		return "not specified"
	}
	return s
}
