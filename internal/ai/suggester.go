package ai

import (
	"errors"

	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// ErrNotImplemented is returned by Suggester methods that have not yet been built out.
var ErrNotImplemented = errors.New("not implemented")

// Suggester analyses a user's approval/rejection history and proposes additions
// to the watchlist (shows.json). It is a distinct AI subsystem from Scorer and
// Enricher — it operates over the full activity corpus rather than per-item, and
// its output is candidate ShowRules for human review, never auto-written.
//
// Intended workflow:
//  1. Client POSTs to /api/suggestions
//  2. Server calls Suggest(history) with the full activity log
//  3. Suggester sends a structured prompt asking the LLM: "given these
//     approved/rejected titles, what similar content might the user want
//     to track that isn't already in their watchlist?"
//  4. LLM returns a JSON array of candidate shows with rationale
//  5. Handler returns suggestions to the UI for human review
//  6. User accepts/dismisses — accepted items are appended to shows.json
//     via a separate write endpoint (not yet implemented)
//
// Note: external metadata enrichment (TVDB/TMDb/OMDb) will eventually feed
// into the suggestion prompt to ground recommendations in real content metadata
// (genre, network, creator, etc.). That path is deferred until a metadata
// provider is selected.
type Suggester struct {
	provider Provider
	logger   *zap.Logger
}

// NewSuggester creates a Suggester backed by the given Provider.
// Pass a non-nil logger to enable structured LLM I/O logging.
func NewSuggester(p Provider, logger *zap.Logger) *Suggester {
	return &Suggester{provider: p, logger: logger}
}

// Suggest analyses the provided activity history and returns candidate ShowRules
// that the user might want to add to their watchlist.
//
// Not yet implemented — returns ErrNotImplemented.
// The method signature is intentionally stable so callers can be wired up now.
func (s *Suggester) Suggest(history []models.Activity, existing []models.ShowRule) ([]models.ShowRule, error) {
	return nil, ErrNotImplemented
}
