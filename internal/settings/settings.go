// Package settings manages runtime-configurable application settings backed by SQLite.
// Values in the database take priority over environment-variable defaults, which in turn
// take priority over the hardcoded fallbacks defined here.
package settings

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/killakam3084/rss-curator/internal/storage"
)

// ──────────────────────────────────────────────────────────────────────────────
// Data types
// ──────────────────────────────────────────────────────────────────────────────

// AppSettings is the full runtime configuration available for live editing.
type AppSettings struct {
	Scheduler SchedulerSettings `json:"scheduler"`
	Alerts    AlertSettings     `json:"alerts"`
	Match     MatchSettings     `json:"match"`
	Auth      AuthSettings      `json:"auth"`
}

// SchedulerSettings controls periodic background tasks.
type SchedulerSettings struct {
	FeedCheckIntervalSecs  int  `json:"feed_check_interval_secs"`
	FeedCheckEnabled       bool `json:"feed_check_enabled"`
	RescoreBackfillEnabled bool `json:"rescore_backfill_enabled"`
}

// AlertSettings controls the alert poller and progress reporting.
type AlertSettings struct {
	AlertPollerIntervalSecs int `json:"alert_poller_interval_secs"`
	ProgressInterval        int `json:"progress_interval"`
}

// MatchSettings controls the default matching rules applied when a show has no
// per-show override.
type MatchSettings struct {
	MinQuality      string   `json:"min_quality"`
	PreferredCodec  string   `json:"preferred_codec"`
	ExcludeGroups   []string `json:"exclude_groups"`
	PreferredGroups []string `json:"preferred_groups"`
}

// AuthSettings controls HTTP basic-auth credentials.
// Password is stored in plaintext — same threat model as the env file (self-hosted only).
type AuthSettings struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// EnvDefaults carries the values parsed from environment variables at startup.
// Fields with zero/empty values mean "the env var was absent; use hardcoded default".
type EnvDefaults struct {
	FeedCheckIntervalSecs int
	ProgressInterval      int
	MinQuality            string
	PreferredCodec        string
	ExcludeGroups         []string
	PreferredGroups       []string
	AuthUsername          string
	AuthPassword          string
}

// ──────────────────────────────────────────────────────────────────────────────
// DB key constants
// ──────────────────────────────────────────────────────────────────────────────

const (
	keyFeedCheckIntervalSecs   = "scheduler.feed_check_interval_secs"
	keyFeedCheckEnabled        = "scheduler.feed_check_enabled"
	keyRescoreBackfillEnabled  = "scheduler.rescore_backfill_enabled"
	keyAlertPollerIntervalSecs = "alerts.alert_poller_interval_secs"
	keyProgressInterval        = "alerts.progress_interval"
	keyMinQuality              = "match.min_quality"
	keyPreferredCodec          = "match.preferred_codec"
	keyExcludeGroups           = "match.exclude_groups"
	keyPreferredGroups         = "match.preferred_groups"
	keyAuthUsername            = "auth.username"
	keyAuthPassword            = "auth.password"
)

// ──────────────────────────────────────────────────────────────────────────────
// Hardcoded fallbacks (lowest priority)
// ──────────────────────────────────────────────────────────────────────────────

func hardcodedDefaults() AppSettings {
	return AppSettings{
		Scheduler: SchedulerSettings{
			FeedCheckIntervalSecs:  3600,
			FeedCheckEnabled:       true,
			RescoreBackfillEnabled: false,
		},
		Alerts: AlertSettings{
			AlertPollerIntervalSecs: 15,
			ProgressInterval:        5,
		},
		Match: MatchSettings{
			MinQuality:      "1080p",
			PreferredCodec:  "x265",
			ExcludeGroups:   []string{},
			PreferredGroups: []string{},
		},
		Auth: AuthSettings{
			Username: "curator",
			Password: "",
		},
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Manager
// ──────────────────────────────────────────────────────────────────────────────

// Manager holds an in-memory cache of AppSettings protected by a RWMutex.
// Use Load to populate from the database, Get for lock-free reads on the hot
// path, and Update to persist and apply changes atomically.
type Manager struct {
	mu      sync.RWMutex
	current AppSettings
	store   storage.Store
}

// NewManager creates a Manager. Call Load before using Get.
func NewManager(store storage.Store) *Manager {
	return &Manager{
		store:   store,
		current: hardcodedDefaults(),
	}
}

// Load reads all persisted settings from the database. env provides values from
// environment variables that override hardcoded defaults when no DB row exists
// yet. It is safe to call Load multiple times.
func (m *Manager) Load(env EnvDefaults) error {
	// Start with hardcoded defaults then apply env overrides.
	s := hardcodedDefaults()
	applyEnvDefaults(&s, env)

	// DB values have highest priority.
	stored, err := m.store.GetAllSettings()
	if err != nil {
		return fmt.Errorf("settings: load from db: %w", err)
	}
	applyStoredValues(&s, stored)

	m.mu.Lock()
	m.current = s
	m.mu.Unlock()
	return nil
}

// Get returns a snapshot of the current settings under a read lock.
func (m *Manager) Get() AppSettings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// Update validates patch, persists each field to the database, and updates the
// in-memory cache atomically. A Password value of "***" means "do not change".
func (m *Manager) Update(patch AppSettings) error {
	if err := validate(patch); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	next := patch
	if next.Auth.Password == "***" {
		next.Auth.Password = m.current.Auth.Password
	}

	if err := m.persist(next); err != nil {
		return err
	}
	m.current = next
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

func validate(s AppSettings) error {
	if s.Scheduler.FeedCheckIntervalSecs <= 0 {
		return fmt.Errorf("settings: scheduler.feed_check_interval_secs must be > 0")
	}
	if s.Alerts.AlertPollerIntervalSecs <= 0 {
		return fmt.Errorf("settings: alerts.alert_poller_interval_secs must be > 0")
	}
	if s.Alerts.ProgressInterval <= 0 {
		return fmt.Errorf("settings: alerts.progress_interval must be > 0")
	}
	return nil
}

// persist writes all fields to the database. Called with m.mu held.
func (m *Manager) persist(s AppSettings) error {
	type kv struct{ key, val string }
	excJSON, _ := json.Marshal(s.Match.ExcludeGroups)
	prefJSON, _ := json.Marshal(s.Match.PreferredGroups)

	pairs := []kv{
		{keyFeedCheckIntervalSecs, fmt.Sprintf("%d", s.Scheduler.FeedCheckIntervalSecs)},
		{keyFeedCheckEnabled, boolStr(s.Scheduler.FeedCheckEnabled)},
		{keyRescoreBackfillEnabled, boolStr(s.Scheduler.RescoreBackfillEnabled)},
		{keyAlertPollerIntervalSecs, fmt.Sprintf("%d", s.Alerts.AlertPollerIntervalSecs)},
		{keyProgressInterval, fmt.Sprintf("%d", s.Alerts.ProgressInterval)},
		{keyMinQuality, s.Match.MinQuality},
		{keyPreferredCodec, s.Match.PreferredCodec},
		{keyExcludeGroups, string(excJSON)},
		{keyPreferredGroups, string(prefJSON)},
		{keyAuthUsername, s.Auth.Username},
		{keyAuthPassword, s.Auth.Password},
	}
	for _, p := range pairs {
		if err := m.store.SetSetting(p.key, p.val); err != nil {
			return fmt.Errorf("settings: persist %s: %w", p.key, err)
		}
	}
	return nil
}

func applyEnvDefaults(s *AppSettings, env EnvDefaults) {
	if env.FeedCheckIntervalSecs > 0 {
		s.Scheduler.FeedCheckIntervalSecs = env.FeedCheckIntervalSecs
	}
	if env.ProgressInterval > 0 {
		s.Alerts.ProgressInterval = env.ProgressInterval
	}
	if env.MinQuality != "" {
		s.Match.MinQuality = env.MinQuality
	}
	if env.PreferredCodec != "" {
		s.Match.PreferredCodec = env.PreferredCodec
	}
	if len(env.ExcludeGroups) > 0 {
		s.Match.ExcludeGroups = env.ExcludeGroups
	}
	if len(env.PreferredGroups) > 0 {
		s.Match.PreferredGroups = env.PreferredGroups
	}
	if env.AuthUsername != "" {
		s.Auth.Username = env.AuthUsername
	}
	if env.AuthPassword != "" {
		s.Auth.Password = env.AuthPassword
	}
}

func applyStoredValues(s *AppSettings, stored map[string]string) {
	if v, ok := stored[keyFeedCheckIntervalSecs]; ok {
		if n := parseInt(v); n > 0 {
			s.Scheduler.FeedCheckIntervalSecs = n
		}
	}
	if v, ok := stored[keyFeedCheckEnabled]; ok {
		s.Scheduler.FeedCheckEnabled = v == "true"
	}
	if v, ok := stored[keyRescoreBackfillEnabled]; ok {
		s.Scheduler.RescoreBackfillEnabled = v == "true"
	}
	if v, ok := stored[keyAlertPollerIntervalSecs]; ok {
		if n := parseInt(v); n > 0 {
			s.Alerts.AlertPollerIntervalSecs = n
		}
	}
	if v, ok := stored[keyProgressInterval]; ok {
		if n := parseInt(v); n > 0 {
			s.Alerts.ProgressInterval = n
		}
	}
	if v, ok := stored[keyMinQuality]; ok && v != "" {
		s.Match.MinQuality = v
	}
	if v, ok := stored[keyPreferredCodec]; ok && v != "" {
		s.Match.PreferredCodec = v
	}
	if v, ok := stored[keyExcludeGroups]; ok {
		var arr []string
		if json.Unmarshal([]byte(v), &arr) == nil {
			s.Match.ExcludeGroups = arr
		}
	}
	if v, ok := stored[keyPreferredGroups]; ok {
		var arr []string
		if json.Unmarshal([]byte(v), &arr) == nil {
			s.Match.PreferredGroups = arr
		}
	}
	if v, ok := stored[keyAuthUsername]; ok && v != "" {
		s.Auth.Username = v
	}
	if v, ok := stored[keyAuthPassword]; ok {
		s.Auth.Password = v
	}
}

func parseInt(s string) int {
	n := 0
	fmt.Sscanf(s, "%d", &n)
	return n
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
