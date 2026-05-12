package models

import (
	"encoding/json"
	"time"
)

// ContentType identifies whether a feed item or rule is for a show or a movie.
type ContentType string

const (
	ContentTypeShow  ContentType = "show"
	ContentTypeMovie ContentType = "movie"
)

// FeedConfig pairs an RSS feed URL with the content type it carries.
type FeedConfig struct {
	URL         string      `yaml:"url"`
	ContentType ContentType `yaml:"content_type"`
}

// FeedItem represents a parsed RSS feed item
type FeedItem struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	GUID        string    `json:"guid"`
	PubDate     time.Time `json:"pub_date"`
	Size        int64     `json:"size"`
	Description string    `json:"description"`

	// Parsed metadata
	ContentType  ContentType `json:"content_type"`
	ShowName     string      `json:"show_name"`
	Season       int         `json:"season"`
	Episode      int         `json:"episode"`
	ReleaseYear  int         `json:"release_year,omitempty"`
	Quality      string      `json:"quality"`
	Codec        string      `json:"codec"`
	Source       string      `json:"source"`
	ReleaseGroup string      `json:"release_group"`
	HDR          []string    `json:"hdr,omitempty"`
}

// StagedTorrent represents a torrent waiting for approval
type StagedTorrent struct {
	ID          int        `json:"id"`
	FeedItem    FeedItem   `json:"feed_item"`
	MatchReason string     `json:"match_reason"`
	StagedAt    time.Time  `json:"staged_at"`
	Status      string     `json:"status"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
	AIScore     float64    `json:"ai_score"`
	AIReason    string     `json:"ai_reason"`
	AIScored    bool       `json:"ai_scored"`
	// MatchConfidence is the scorer's assessment of whether the matched rule name
	// plausibly describes the actual content in the title. -1 means not yet assessed.
	// Orthogonal to AIScore: a perfect release of wrong content → high score, low confidence.
	MatchConfidence       float64 `json:"match_confidence"`
	MatchConfidenceReason string  `json:"match_confidence_reason"`
	// FailReason holds the error message from the last failed qBittorrent add attempt.
	// Non-empty only when Status == "failed".
	FailReason string `json:"fail_reason,omitempty"`
}

// RawFeedItem represents a raw item pulled from RSS feed (before filtering/matching)
// This is temporary data for UI visibility into feed discovery process
type RawFeedItem struct {
	ID        int       `json:"id"`
	FeedItem  FeedItem  `json:"feed_item"`
	PulledAt  time.Time `json:"pulled_at"`
	ExpiresAt time.Time `json:"expires_at"` // TTL for cleanup
}

// Activity represents an action taken on a torrent (approve/reject)
type Activity struct {
	ID           int       `json:"id"`
	TorrentID    int       `json:"torrent_id"`
	TorrentTitle string    `json:"torrent_title"`
	Action       string    `json:"action"` // "approve" or "reject"
	ActionAt     time.Time `json:"action_at"`
	MatchReason  string    `json:"match_reason"`
}

// ShowRule represents rules for a specific show
type ShowRule struct {
	Name            string   `json:"name"`
	MinQuality      string   `json:"min_quality,omitempty"`
	PreferredCodec  string   `json:"preferred_codec,omitempty"`
	PreferredGroups []string `json:"preferred_groups,omitempty"`
	PreferredHDR    []string `json:"preferred_hdr,omitempty"`
	ExcludeGroups   []string `json:"exclude_groups,omitempty"`
}

// MovieRule represents rules for a specific movie (mirrors ShowRule).
type MovieRule struct {
	Name            string   `json:"name"`
	MinQuality      string   `json:"min_quality,omitempty"`
	PreferredCodec  string   `json:"preferred_codec,omitempty"`
	PreferredGroups []string `json:"preferred_groups,omitempty"`
	PreferredHDR    []string `json:"preferred_hdr,omitempty"`
	ExcludeGroups   []string `json:"exclude_groups,omitempty"`
}

// DefaultRules represents default matching rules
type DefaultRules struct {
	MinQuality      string   `json:"min_quality"`
	PreferredCodec  string   `json:"preferred_codec"`
	PreferredGroups []string `json:"preferred_groups"`
	PreferredHDR    []string `json:"preferred_hdr,omitempty"`
	ExcludeGroups   []string `json:"exclude_groups"`
}

// ShowsConfig represents the watchlist.json structure
type ShowsConfig struct {
	Shows    []ShowRule   `json:"shows"`
	Movies   []MovieRule  `json:"movies"`
	Defaults DefaultRules `json:"defaults"`
}

// MatchRule represents legacy rule structure
type MatchRule struct {
	ShowNames       []string `yaml:"show_names"`
	MinQuality      string   `yaml:"min_quality"`
	PreferredCodec  string   `yaml:"preferred_codec"`
	ExcludeGroups   []string `yaml:"exclude_groups"`
	PreferredGroups []string `yaml:"preferred_groups"`
}

// Config represents application configuration
type Config struct {
	FeedURLs      []string     `yaml:"feed_urls"`
	MovieFeedURLs []string     `yaml:"movie_feed_urls"`
	Feeds         []FeedConfig `yaml:"-"` // built from FeedURLs + MovieFeedURLs in loadConfig
	PollInterval  int          `yaml:"poll_interval"`
	QBittorrent   QBConfig     `yaml:"qbittorrent"`
	MatchRules    MatchRule    `yaml:"match_rules"`
	StoragePath   string       `yaml:"storage_path"`
	ShowsConfig   *ShowsConfig
}

// QBConfig represents qBittorrent connection settings
type QBConfig struct {
	Host      string `yaml:"host"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	Category  string `yaml:"category"`   // Default category for downloads
	SavePath  string `yaml:"save_path"`  // Default save path
	AddPaused bool   `yaml:"add_paused"` // Add torrents in paused state
}

// JobSummary is used internally by FailJob / MarkStaleJobsFailed to store an
// error message. Completed jobs use per-type summary structs (see below).
type JobSummary struct {
	ErrorMessage string `json:"error_message,omitempty"`
}

// FeedCheckSummary is the summary stored for "feed_check" jobs.
type FeedCheckSummary struct {
	ItemsFound   int    `json:"items_found"`
	ItemsMatched int    `json:"items_matched"`
	ItemsScored  int    `json:"items_scored"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// RescoreSummary is the summary stored for "rescore" jobs.
type RescoreSummary struct {
	ItemsScored  int    `json:"items_scored"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// RescoreBackfillSummary is the summary stored for "rescore_backfill" jobs.
type RescoreBackfillSummary struct {
	ItemsScored  int    `json:"items_scored"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// RematchSummary is the summary stored for "rematch" jobs.
// JSON field names are kept compatible with what the UI already reads.
type RematchSummary struct {
	ItemsProcessed     int    `json:"items_found"`           // total submitted
	ItemsRematched     int    `json:"items_matched"`         // still match a rule
	ItemsNoLongerMatch int    `json:"items_no_longer_match"` // transitioned to rejected
	ItemsRescored      int    `json:"items_scored"`          // re-scored during rematch
	ErrorMessage       string `json:"error_message,omitempty"`
}

// SuggestRefreshSummary is the summary stored for "suggest_refresh" jobs.
type SuggestRefreshSummary struct {
	SuggestionsGenerated int    `json:"suggestions_generated"`
	ErrorMessage         string `json:"error_message,omitempty"`
}

// WatchlistEnrichSummary is the summary stored for "watchlist_enrich" jobs.
type WatchlistEnrichSummary struct {
	ShowsUpdated  int    `json:"shows_updated"`
	MoviesUpdated int    `json:"movies_updated"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

// AlertRecord is an ephemeral in-memory notification emitted by the server for
// user-facing events. It is never persisted to SQLite; the activity_log table
// remains the durable audit trail.
//
// Action values:
//   - "approve"    — torrent accepted by a user
//   - "reject"     — torrent rejected by a user
//   - "queue"      — accepted torrent pushed to qBittorrent
//   - "staged"     — feed_check completed with new matches
//   - "job_failed" — any background job failed
type AlertRecord struct {
	ID           uint64    `json:"id"`
	Action       string    `json:"action"`
	TorrentID    int       `json:"torrent_id,omitempty"`
	TorrentTitle string    `json:"torrent_title,omitempty"`
	MatchReason  string    `json:"match_reason,omitempty"`
	Message      string    `json:"message"`
	TriggeredAt  time.Time `json:"triggered_at"`
	Dismissed    bool      `json:"dismissed,omitempty"`
}

// JobRecord represents a tracked background operation written to the jobs table.
// Status values: "running", "completed", "failed", "cancelled".
// Type values: "feed_check", "rescore_backfill", "rescore", "rematch", "suggest_refresh".
type JobRecord struct {
	ID          int             `json:"id"`
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Progress    string          `json:"progress,omitempty"` // human-readable mid-job status; only set on running events
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Summary     json.RawMessage `json:"summary,omitempty"`
}
