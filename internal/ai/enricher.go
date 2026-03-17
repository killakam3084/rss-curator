package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

const enrichSystemPrompt = `You are a media title parser. Given a torrent title, extract structured metadata.
Always respond with a single JSON object. No explanation, no markdown, just raw JSON.
Fields:
  show_name  (string)  - human-readable show name, e.g. "The Last of Us"
  season     (int)     - season number, 0 if unknown
  episode    (int)     - episode number, 0 if unknown
  year       (int)     - release year, 0 if unknown
  quality    (string)  - one of 2160P, 1080P, 720P, 4K, or "" if unknown
  codec      (string)  - one of x265, x264, or "" if unknown
  source     (string)  - e.g. WEB-DL, BluRay, HDTV, WEBRip, AMZN, NF, DSNP, HMAX, ATVP, or "" if unknown
  release_group (string) - release group suffix, e.g. NTb, FLUX, or "" if unknown`

type enrichResult struct {
	ShowName     string `json:"show_name"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	Year         int    `json:"year"`
	Quality      string `json:"quality"`
	Codec        string `json:"codec"`
	Source       string `json:"source"`
	ReleaseGroup string `json:"release_group"`
}

// Enricher uses an LLM to fill in FeedItem fields the regex parser could not determine.
// It is intentionally a fallback - it only fires when one or more key metadata
// fields are missing.
type Enricher struct {
	provider    Provider
	timeoutSecs int         // per-request LLM timeout; read from CURATOR_AI_TIMEOUT_SECS
	logger      *zap.Logger // may be nil; logging silently skipped when nil
}

// NewEnricher creates an Enricher backed by the given Provider.
// The per-request LLM timeout is read from CURATOR_AI_TIMEOUT_SECS (default 60).
// Pass a non-nil logger to enable structured LLM I/O logging (e.g. for log drawer visibility).
// Pass nil for CLI/check paths where stdout is the observable surface.
func NewEnricher(p Provider, logger *zap.Logger) *Enricher {
	timeout := 60
	if v := os.Getenv("CURATOR_AI_TIMEOUT_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = n
		}
	}
	return &Enricher{provider: p, timeoutSecs: timeout, logger: logger}
}

// Enrich attempts to fill in missing title metadata using the LLM.
// It is safe to call on every item - it no-ops if the regex already parsed the
// title successfully, or if the provider is unreachable.
func (e *Enricher) Enrich(item *models.FeedItem) {
	e.enrich(item, false)
}

// EnrichForce forces an AI parse even when regex-derived fields are already
// populated, and lets non-empty AI fields override the current values. This is
// useful for rematch/debug flows where we want to compare deterministic parsing
// with a fresh model interpretation of the title.
func (e *Enricher) EnrichForce(item *models.FeedItem) {
	e.enrich(item, true)
}

func (e *Enricher) enrich(item *models.FeedItem, force bool) {
	// Only enrich if at least one key field is missing.
	if !force && item.ShowName != "" &&
		item.Season > 0 &&
		item.Quality != "" &&
		item.Codec != "" &&
		item.Source != "" &&
		item.ReleaseGroup != "" {
		return
	}

	if !e.provider.Available() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.timeoutSecs)*time.Second)
	defer cancel()

	userPrompt := fmt.Sprintf("Title: %s", item.Title)

	if e.logger != nil {
		e.logger.Debug("enricher.request",
			zap.String("title", item.Title),
			zap.Bool("force", force),
			zap.String("user_prompt", userPrompt),
		)
	}

	start := time.Now()
	content, err := e.provider.Complete(ctx, enrichSystemPrompt, userPrompt)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		if e.logger != nil {
			e.logger.Debug("enricher.response",
				zap.String("title", item.Title),
				zap.Int64("duration_ms", durationMs),
				zap.Error(err),
			)
		}
		return // silent fallback - item unchanged
	}

	// Strip markdown fences if the model wrapped the response.
	raw := content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result enrichResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		if e.logger != nil {
			e.logger.Debug("enricher.response",
				zap.String("title", item.Title),
				zap.Int64("duration_ms", durationMs),
				zap.String("raw_response", raw),
				zap.String("parse_error", err.Error()),
			)
		}
		return // silent fallback
	}

	if e.logger != nil {
		e.logger.Debug("enricher.response",
			zap.String("title", item.Title),
			zap.Bool("force", force),
			zap.Int64("duration_ms", durationMs),
			zap.String("raw_response", raw),
			zap.String("show_name", result.ShowName),
			zap.Int("season", result.Season),
			zap.Int("episode", result.Episode),
			zap.String("quality", result.Quality),
			zap.String("codec", result.Codec),
			zap.String("source", result.Source),
			zap.String("release_group", result.ReleaseGroup),
		)
	}

	if result.ShowName != "" && (force || item.ShowName == "") {
		item.ShowName = result.ShowName
	}
	if result.Season > 0 && (force || item.Season == 0) {
		item.Season = result.Season
	}
	if result.Episode > 0 && (force || item.Episode == 0) {
		item.Episode = result.Episode
	}
	if result.Quality != "" && (force || item.Quality == "") {
		item.Quality = strings.ToUpper(strings.TrimSpace(result.Quality))
	}
	if result.Codec != "" && (force || item.Codec == "") {
		codec := strings.ToUpper(strings.TrimSpace(result.Codec))
		if strings.Contains(codec, "265") || codec == "HEVC" {
			item.Codec = "x265"
		} else if strings.Contains(codec, "264") {
			item.Codec = "x264"
		}
	}
	if result.Source != "" && (force || item.Source == "") {
		item.Source = strings.TrimSpace(result.Source)
	}
	if result.ReleaseGroup != "" && (force || item.ReleaseGroup == "") {
		item.ReleaseGroup = strings.TrimSpace(result.ReleaseGroup)
	}
}
