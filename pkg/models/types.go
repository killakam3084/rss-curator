package models

import "time"

// FeedItem represents a parsed RSS feed item
type FeedItem struct {
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	GUID        string    `json:"guid"`
	PubDate     time.Time `json:"pub_date"`
	Size        int64     `json:"size"`
	Description string    `json:"description"`

	// Parsed metadata
	ShowName     string `json:"show_name"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	Quality      string `json:"quality"`
	Codec        string `json:"codec"`
	Source       string `json:"source"`
	ReleaseGroup string `json:"release_group"`
}

// StagedTorrent represents a torrent waiting for approval
type StagedTorrent struct {
	ID          int        `json:"id"`
	FeedItem    FeedItem   `json:"feed_item"`
	MatchReason string     `json:"match_reason"`
	StagedAt    time.Time  `json:"staged_at"`
	Status      string     `json:"status"`
	ApprovedAt  *time.Time `json:"approved_at,omitempty"`
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
	ExcludeGroups   []string `json:"exclude_groups,omitempty"`
}

// DefaultRules represents default matching rules
type DefaultRules struct {
	MinQuality      string   `json:"min_quality"`
	PreferredCodec  string   `json:"preferred_codec"`
	PreferredGroups []string `json:"preferred_groups"`
	ExcludeGroups   []string `json:"exclude_groups"`
}

// ShowsConfig represents the shows.json structure
type ShowsConfig struct {
	Shows    []ShowRule   `json:"shows"`
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
	FeedURLs     []string  `yaml:"feed_urls"`
	PollInterval int       `yaml:"poll_interval"`
	QBittorrent  QBConfig  `yaml:"qbittorrent"`
	MatchRules   MatchRule `yaml:"match_rules"`
	StoragePath  string    `yaml:"storage_path"`
	ShowsConfig  *ShowsConfig
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
