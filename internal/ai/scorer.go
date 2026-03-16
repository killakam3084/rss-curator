package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

const scoreSystemPrompt = `You are a torrent preference scorer.
You will receive:
  1. A recent approval history showing past APPROVE/REJECT/QUEUE decisions (for context only).
  2. A candidate torrent to score — this is the release you must evaluate.

Scoring rules:
  1. Score ONLY the candidate torrent. The history entries are context — do not score them.
  2. The "Match reason" field is AUTHORITATIVE — it was produced by a deterministic
     matcher that already confirmed title, quality, codec, and group meet preferences.
     Do NOT re-evaluate those specs; treat a non-empty match reason as confirmation.
  3. Use history to calibrate likelihood of approval for this type of content and quality.
  4. A torrent matching well on content with suboptimal technical specs should score
     higher than one with perfect technical specs but weak content relevance.

Match confidence:
  Assess whether the "Matched rule" field plausibly identifies the actual content in the
  candidate title. Score 1.0 when the rule clearly names the show/movie being released.
  Score low when the rule appears to be an incidental substring of an unrelated title
  (e.g. rule "NOVA" firing on "Renovation"; rule "Invincible" firing on "The Invincible
  Samurai"). This is orthogonal to release quality.

Always respond with a single JSON object. No explanation, no markdown, just raw JSON.
Fields:
  score                    (float, 0.0-1.0)   - predicted likelihood of approval
  reason                   (string)            - one-line explanation of score
  match_confidence         (float, 0.0-1.0)   - likelihood the rule name correctly identifies the content
  match_confidence_reason  (string)            - one-line explanation of match confidence

Example output (use exactly this shape, no other keys):
{"score":0.85,"reason":"Strong content match, preferred codec and group","match_confidence":0.95,"match_confidence_reason":"Rule name exactly matches parsed show title"}`

type scoreResult struct {
	Score                 float64 `json:"score"`
	Reason                string  `json:"reason"`
	MatchConfidence       float64 `json:"match_confidence"`
	MatchConfidenceReason string  `json:"match_confidence_reason"`
}

// Scorer ranks matched StagedTorrents using the LLM and the user's activity history.
type Scorer struct {
	provider    Provider
	historySize int         // number of activity entries to sample into the prompt context
	timeoutSecs int         // per-request LLM timeout; read from CURATOR_AI_TIMEOUT_SECS
	logger      *zap.Logger // may be nil; set via SetLogger after construction
}

// scoreOutputSchema is an Ollama structured-output JSON Schema that pins the
// model response to exactly the four fields the scorer expects. Passed via
// FormatSetter.SetFormat so the model cannot generate an off-schema response.
var scoreOutputSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"score": {
			"type": "number",
			"minimum": 0,
			"maximum": 1
		},
		"reason":                   {"type": "string"},
		"match_confidence": {
			"type": "number",
			"minimum": 0,
			"maximum": 1
		},
		"match_confidence_reason":  {"type": "string"}
	},
	"required": ["score", "reason", "match_confidence", "match_confidence_reason"]
}`)

// NewScorer creates a Scorer backed by the given Provider.
// The history window size is read from CURATOR_AI_HISTORY_SIZE (default 40).
// The per-request LLM timeout is read from CURATOR_AI_TIMEOUT_SECS (default 60).
// Call SetLogger to enable structured LLM I/O logging.
func NewScorer(p Provider) *Scorer {
	size := 40
	if v := os.Getenv("CURATOR_AI_HISTORY_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			size = n
		}
	}
	timeout := 60
	if v := os.Getenv("CURATOR_AI_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	s := &Scorer{provider: p, historySize: size, timeoutSecs: timeout}
	s.configureFormat()
	return s
}

// configureFormat sets the structured output schema on the provider if it
// supports FormatSetter (i.e. Ollama). This pins the model response to the
// exact four fields the scorer expects, preventing off-schema hallucinations.
func (s *Scorer) configureFormat() {
	if fs, ok := s.provider.(FormatSetter); ok {
		fs.SetFormat(scoreOutputSchema)
	}
}

// SetLogger wires a zap.Logger into the scorer so that all LLM requests and
// responses are emitted as DEBUG-level structured log events. When the logger
// is nil (the default) all I/O logging is silently skipped.
func (s *Scorer) SetLogger(l *zap.Logger) { s.logger = l }

// ScoreAll attaches AI scores to each staged torrent.
// Torrents that fail scoring retain AIScore=0 and AIReason="".
// Returns the same slice with scores filled in.
func (s *Scorer) ScoreAll(staged []models.StagedTorrent, history []models.Activity) []models.StagedTorrent {
	if !s.provider.Available() {
		return staged
	}
	for i := range staged {
		t := &staged[i]
		candidateShow := extractMatchedRule(t.MatchReason)
		histCtx := buildHistoryContext(history, s.historySize, candidateShow)
		t.AIScore, t.AIReason, t.MatchConfidence, t.MatchConfidenceReason = s.scoreOne(t, histCtx)
	}
	return staged
}

// extractMatchedRule parses the first "matches show: NAME" segment from a
// match reason string (e.g. "matches show: NOVA, quality: 1080P") and returns
// just the rule name ("NOVA"). Returns the full reason unchanged if no show
// segment is found (e.g. quality-only or group-only match reasons).
func extractMatchedRule(matchReason string) string {
	const prefix = "matches show: "
	idx := strings.Index(matchReason, prefix)
	if idx < 0 {
		return matchReason
	}
	rest := matchReason[idx+len(prefix):]
	if comma := strings.Index(rest, ","); comma >= 0 {
		return strings.TrimSpace(rest[:comma])
	}
	return strings.TrimSpace(rest)
}

func (s *Scorer) scoreOne(t *models.StagedTorrent, histCtx string) (float64, string, float64, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.timeoutSecs)*time.Second)
	defer cancel()

	showEp := ""
	if t.FeedItem.ShowName != "" {
		showEp = t.FeedItem.ShowName
		if t.FeedItem.Season > 0 || t.FeedItem.Episode > 0 {
			showEp += fmt.Sprintf(" S%02dE%02d", t.FeedItem.Season, t.FeedItem.Episode)
		}
	}

	user := fmt.Sprintf(
		"Recent approval history (for context — do not score these):\n%s\nCandidate torrent to score:\nTitle: %s\nParsed show (from title): %s\nMatched rule: %s\nMatch reason: %s\nQuality: %s | Codec: %s | Group: %s | Source: %s\n\nScore the candidate torrent above.",
		histCtx,
		t.FeedItem.Title,
		showEp,
		extractMatchedRule(t.MatchReason),
		t.MatchReason,
		t.FeedItem.Quality,
		t.FeedItem.Codec,
		t.FeedItem.ReleaseGroup,
		t.FeedItem.Source,
	)

	// Log scorer.request with compressed history context for correlation
	if s.logger != nil {
		s.logger.Debug("scorer.request",
			zap.Int("torrent_id", t.ID),
			zap.String("title", t.FeedItem.Title),
			zap.String("user_prompt", user),
			zap.String("compressed_history", histCtx),
		)
	}

	if s.logger != nil {
		s.logger.Debug("scorer.request",
			zap.Int("torrent_id", t.ID),
			zap.String("title", t.FeedItem.Title),
			zap.String("user_prompt", user),
		)
	}

	start := time.Now()
	content, err := s.provider.Complete(ctx, scoreSystemPrompt, user)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		if s.logger != nil {
			s.logger.Debug("scorer.response",
				zap.Int("torrent_id", t.ID),
				zap.Int64("duration_ms", durationMs),
				zap.Error(err),
			)
		}
		return 0, "", -1, ""
	}

	raw := content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result scoreResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		if s.logger != nil {
			s.logger.Debug("scorer.response",
				zap.Int("torrent_id", t.ID),
				zap.Int64("duration_ms", durationMs),
				zap.String("raw_response", raw),
				zap.String("parse_error", err.Error()),
			)
		}
		return 0, "", -1, ""
	}

	// Clamp score to [0, 1] and record original for diagnostics.
	origScore := result.Score
	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 1 {
		result.Score = 1
	}
	if s.logger != nil && origScore != result.Score {
		s.logger.Debug("scorer.clamped",
			zap.Int("torrent_id", t.ID),
			zap.Float64("original_score", origScore),
			zap.Float64("clamped_score", result.Score),
		)
	}

	// Clamp match_confidence to [0, 1]; -1 sentinel means not assessed.
	if result.MatchConfidence >= 0 {
		if result.MatchConfidence < 0 {
			result.MatchConfidence = 0
		}
		if result.MatchConfidence > 1 {
			result.MatchConfidence = 1
		}
	}

	if s.logger != nil {
		s.logger.Debug("scorer.response",
			zap.Int("torrent_id", t.ID),
			zap.Int64("duration_ms", durationMs),
			zap.String("raw_response", raw),
			zap.Float64("score", result.Score),
			zap.String("reason", result.Reason),
			zap.Float64("match_confidence", result.MatchConfidence),
			zap.String("match_confidence_reason", result.MatchConfidenceReason),
		)
	}

	return result.Score, result.Reason, result.MatchConfidence, result.MatchConfidenceReason
}

// buildHistoryContext produces a compact text summary of activity history for
// inclusion in the scoring prompt. Uses stratified sampling via sampleHistory.
// Each line includes the action, title, and match reason for richer signal.
func buildHistoryContext(history []models.Activity, size int, candidateShow string) string {
	summaries := BuildShowSummaries(history, size)
	if len(summaries) == 0 {
		return "No history yet."
	}

	// Create a sortable slice of summaries by total interaction weight.
	type kv struct {
		name string
		tot  float64
		s    *ShowSummary
	}
	var list []kv
	for name, s := range summaries {
		tot := s.ApproveWeight + s.RejectWeight + float64(s.QueueCount)
		list = append(list, kv{name: name, tot: tot, s: s})
	}

	sort.Slice(list, func(i, j int) bool { return list[i].tot > list[j].tot })

	var sb strings.Builder
	// Candidate show first when available
	if candidateShow != "" {
		if s, ok := summaries[candidateShow]; ok {
			sb.WriteString(formatShortSummary(s) + "\n")
			// mark printed
			for i := range list {
				if list[i].name == candidateShow {
					list[i].tot = -1
					break
				}
			}
		}
	}

	// Append top 3 other shows
	appended := 0
	for _, it := range list {
		if it.tot < 0 {
			continue
		}
		sb.WriteString(formatShortSummary(it.s) + "\n")
		appended++
		if appended >= 3 {
			break
		}
	}

	return sb.String()
}

// sampleHistory returns a balanced, deduplicated sample of activity history
// for use as scoring context. It draws up to size/2 entries from each of the
// approve and reject pools (deduplicated by title, most recent retained), then
// returns the combined slice sorted chronologically for temporal coherence.
// If one pool is smaller, remaining slots are filled from the other.
func sampleHistory(history []models.Activity, size int) []models.Activity {
	if len(history) == 0 || size <= 0 {
		return history
	}

	var approves, rejects []models.Activity
	for _, h := range history {
		switch strings.ToUpper(h.Action) {
		case "REJECT", "REJECTED":
			rejects = append(rejects, h)
		default: // approve, queue, etc. — positive signal
			approves = append(approves, h)
		}
	}

	// Dedup by title, keeping most recent occurrence.
	dedup := func(entries []models.Activity) []models.Activity {
		seen := make(map[string]struct{}, len(entries))
		out := make([]models.Activity, 0, len(entries))
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if _, ok := seen[e.TorrentTitle]; !ok {
				seen[e.TorrentTitle] = struct{}{}
				out = append(out, e)
			}
		}
		// Reverse to restore chronological order within pool.
		for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
			out[l], out[r] = out[r], out[l]
		}
		return out
	}
	approves = dedup(approves)
	rejects = dedup(rejects)

	half := size / 2
	aCount := half
	rCount := size - half
	if len(approves) < aCount {
		aCount = len(approves)
		rCount = size - aCount
	}
	if len(rejects) < rCount {
		rCount = len(rejects)
	}

	// Take most recent aCount/rCount from each pool.
	if aCount < len(approves) {
		approves = approves[len(approves)-aCount:]
	}
	if rCount < len(rejects) {
		rejects = rejects[len(rejects)-rCount:]
	}

	out := append(approves, rejects...)
	// Sort combined slice by ActionAt ascending so the model sees a temporal narrative.
	sort.Slice(out, func(i, j int) bool {
		return out[i].ActionAt.Before(out[j].ActionAt)
	})
	return out
}
