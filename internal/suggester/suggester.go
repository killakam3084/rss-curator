package suggester

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
)

// suggestOutputSchema pins Ollama to the exact JSON shape we expect, preventing
// preamble/explanation text from polluting the response.
var suggestOutputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"suggestions": {
			"type": "array",
			"items": {
				"type": "object",
				"properties": {
					"show_name": {"type": "string"},
					"reason":    {"type": "string"},
					"quality":   {"type": "string"},
					"codec":     {"type": "string"}
				},
				"required": ["show_name", "reason", "quality", "codec"]
			}
		}
	},
	"required": ["suggestions"]
}`)

const systemPrompt = `You are a TV/movie watchlist assistant recommending new shows to add.

Given: the user's watchlist (with genre/network metadata), recent approvals (taste signals), and quality preferences.

Rules:
- Never suggest a show already in the watchlist.
- Match genre patterns from the watchlist.
- Use the provided quality and codec on every suggestion.
- Respond with raw JSON only — no explanation, no markdown.`

// SuggestionMeta holds metadata fetched from the provider for a suggested show.
// All fields are optional — absent when the provider has no record.
type SuggestionMeta struct {
	ProviderURL  string   `json:"provider_url,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Network      string   `json:"network,omitempty"`
	Status       string   `json:"status,omitempty"`
	PremiereYear int      `json:"premiere_year,omitempty"`
	Overview     string   `json:"overview,omitempty"`
}

// Suggestion is a single LLM-proposed show that the user might want to add.
type Suggestion struct {
	ShowName      string          `json:"show_name"`
	Reason        string          `json:"reason"`
	SuggestedRule models.ShowRule `json:"suggested_rule"`
	Meta          *SuggestionMeta `json:"meta,omitempty"`
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
	// Pin Ollama to structured output so it cannot emit preamble/explanation text.
	if fs, ok := provider.(ai.FormatSetter); ok {
		fs.SetFormat(suggestOutputSchema)
	}

	// Timeout: CURATOR_AI_SUGGESTER_TIMEOUT_SECS > CURATOR_AI_TIMEOUT_SECS > 120.
	// Default is higher than the scorer default (60s) because suggestion prompts
	// are significantly larger (full watchlist + metadata + history).
	timeout := 120
	if v := os.Getenv("CURATOR_AI_SUGGESTER_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	} else if v := os.Getenv("CURATOR_AI_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}

	return &Suggester{
		store:      store,
		provider:   provider,
		matcher:    m,
		metaLookup: lu,
		timeoutSec: timeout,
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
	historyBlock := buildHistoryBlock(history, 10)

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

	suggestions, parseErr := sg.parseResponse(raw, defaultQuality, defaultCodec)
	if parseErr != nil {
		// LLM returned unparseable output — treat as empty, not a hard error.
		// The raw response is included so operators can diagnose prompt issues.
		fmt.Printf("[Suggester] parse error (%v); raw response: %q\n", parseErr, raw)
		return []Suggestion{}, nil
	}

	// Deterministic deduplication: drop any suggestion that already exists in
	// the watchlist. Small models regularly ignore the "never suggest existing
	// shows" instruction regardless of prompt wording — this is a code-level
	// guarantee that no watchlist show leaks through.
	existing := make(map[string]bool, len(cfg.Shows))
	for _, s := range cfg.Shows {
		existing[normalizeName(s.Name)] = true
	}
	filtered := suggestions[:0]
	for _, s := range suggestions {
		if !existing[normalizeName(s.ShowName)] {
			filtered = append(filtered, s)
		}
	}
	suggestions = filtered

	// Enrich each suggestion with provider metadata.
	// Resolve() is cache-first; for new show names this triggers one provider
	// fetch per suggestion — acceptable since this is a manual, infrequent action.
	// Suggestions that fail to resolve are dropped entirely: a real show name
	// will always resolve against TVMaze; hallucinated or malformed names (e.g.
	// "Andor spin-off series, Ahsoka") won't — and we don't want them in results.
	if sg.metaLookup != nil {
		validated := suggestions[:0]
		for i := range suggestions {
			if meta := sg.metaLookup.Resolve(ctx, suggestions[i].ShowName); meta != nil {
				suggestions[i].Meta = &SuggestionMeta{
					ProviderURL:  meta.ProviderURL,
					Genres:       meta.Genres,
					Network:      meta.Network,
					Status:       meta.Status,
					PremiereYear: meta.PremiereYear,
					Overview:     meta.Overview,
				}
				validated = append(validated, suggestions[i])
			}
		}
		suggestions = validated
	}

	return suggestions, nil
}

// buildWatchlistBlock creates the enriched watchlist text block for the prompt.
func (sg *Suggester) buildWatchlistBlock(ctx context.Context, shows []models.ShowRule) string {
	var sb strings.Builder
	for i, show := range shows {
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, show.Name))

		// Attempt metadata enrichment (cache-only — no external quota).
		// Overview is intentionally omitted: it is the largest per-show token
		// contributor and adds little beyond what genres + network already convey.
		if sg.metaLookup != nil {
			if meta := sg.metaLookup.Resolve(ctx, show.Name); meta != nil {
				var parts []string
				if len(meta.Genres) > 0 {
					parts = append(parts, strings.Join(meta.Genres, "/"))
				}
				if meta.Network != "" {
					parts = append(parts, meta.Network)
				}
				if meta.Status != "" {
					parts = append(parts, meta.Status)
				}
				if len(parts) > 0 {
					sb.WriteString(" [" + strings.Join(parts, ", ") + "]")
				}
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildHistoryBlock summarises recent approvals as a concise text block.
// Emits deduplicated show names rather than full torrent titles to keep the
// history section compact — the LLM needs the show name, not the release group.
func buildHistoryBlock(history []models.Activity, maxEntries int) string {
	if len(history) == 0 {
		return "No recent approvals on record.\n"
	}
	seen := make(map[string]bool)
	var sb strings.Builder
	count := 0
	for _, a := range history {
		if count >= maxEntries {
			break
		}
		// Use match_reason show name when available; fall back to full title.
		name := extractShowName(a.MatchReason)
		if name == "" {
			name = a.TorrentTitle
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		sb.WriteString("- " + name + "\n")
		count++
	}
	return sb.String()
}

// extractShowName pulls the show name from a match_reason string like
// "matches show: Breaking Bad, quality: 1080P". Returns "" if not found.
func extractShowName(matchReason string) string {
	const prefix = "matches show: "
	idx := strings.Index(matchReason, prefix)
	if idx < 0 {
		return ""
	}
	rest := matchReason[idx+len(prefix):]
	if comma := strings.Index(rest, ","); comma >= 0 {
		return strings.TrimSpace(rest[:comma])
	}
	return strings.TrimSpace(rest)
}

// parseResponse extracts and unmarshals the LLM JSON. It handles:
//   - Markdown code fences (```json ... ```)
//   - Preamble/postamble text (extract first '{' to last '}')
//   - Falls back to defaults for quality/codec when the LLM omits them.
func (sg *Suggester) parseResponse(raw, defaultQuality, defaultCodec string) ([]Suggestion, error) {
	content := strings.TrimSpace(raw)

	// Strip markdown fences if present.
	if idx := strings.Index(content, "```"); idx >= 0 {
		content = content[idx:]
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		if end := strings.LastIndex(content, "```"); end >= 0 {
			content = content[:end]
		}
		content = strings.TrimSpace(content)
	}

	// Extract the outermost JSON object to survive any remaining preamble.
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	var resp llmResponse
	if err := json.Unmarshal([]byte(content), &resp); err != nil {
		return nil, fmt.Errorf("suggester: could not parse LLM response: %w", err)
	}

	out := make([]Suggestion, 0, len(resp.Suggestions))
	for _, s := range resp.Suggestions {
		if s.ShowName == "" {
			continue
		}
		quality := sanitizeQuality(s.Quality, defaultQuality)
		codec := sanitizeCodec(s.Codec, defaultCodec)
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

// sanitizeQuality extracts the first recognized resolution token from the
// LLM-supplied string (case-insensitive). Falls back to defaultQuality when
// no known token is found. Canonical output is lowercase ("1080p", "2160p").
func sanitizeQuality(raw, defaultQuality string) string {
	known := []string{"2160p", "1080p", "720p", "4k"}
	lower := strings.ToLower(raw)
	for _, q := range known {
		if strings.Contains(lower, q) {
			return q
		}
	}
	if defaultQuality != "" {
		return defaultQuality
	}
	return "1080p"
}

// sanitizeCodec extracts the first recognized codec token from the
// LLM-supplied string (case-insensitive). Falls back to defaultCodec when
// no known token is found. Returns "" (omitted from rule) when both the raw
// value and the default are unrecognized or empty.
func sanitizeCodec(raw, defaultCodec string) string {
	known := []string{"x265", "x264", "h265", "h264", "hevc", "avc", "av1"}
	lower := strings.ToLower(raw)
	for _, c := range known {
		if strings.Contains(lower, c) {
			return c
		}
	}
	// Fall back to default only if it is itself a known codec.
	lowerDefault := strings.ToLower(defaultCodec)
	for _, c := range known {
		if lowerDefault == c {
			return c
		}
	}
	return ""
}

// normalizeName returns a lowercase alphanumeric-only key used for watchlist
// deduplication so that punctuation and casing differences don't cause misses
// (e.g. "Daredevil: Born Again" == "daredevilbornagain").
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
