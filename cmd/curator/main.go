package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/api"
	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/feed"
	"github.com/killakam3084/rss-curator/internal/jobs"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/ops"
	"github.com/killakam3084/rss-curator/internal/scheduler"
	"github.com/killakam3084/rss-curator/internal/settings"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/internal/suggester"
	"github.com/killakam3084/rss-curator/pkg/models"
)

const (
	version = "0.33.1"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Load config
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize storage
	store, err := storage.New(cfg.StoragePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Initialise the TV metadata provider, cache, and lookup resolver.
	// The cache is co-located with the main DB (same directory) so it lands on
	// the same container volume automatically.
	metaProvider := metadata.NewMetadataProvider()
	metaCache, metaCacheErr := metadata.NewCache(cfg.StoragePath)
	if metaCacheErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: metadata cache unavailable: %v\n", metaCacheErr)
	}
	metaLookup := metadata.NewLookup(metaProvider, metaCache)
	if metaCacheErr == nil {
		defer metaCache.Close()
	}

	// Create the log buffer once; shared across all commands that log.
	buf := logbuffer.NewBuffer()

	switch command {
	case "check", "scan":
		cmdCheck(cfg, store, metaLookup)
	case "list", "ls":
		cmdList(store)
	case "approve":
		cmdApprove(cfg, store, os.Args[2:])
	case "reject":
		cmdReject(store, os.Args[2:])
	case "review":
		cmdReview(cfg, store)
	case "serve":
		cmdServe(cfg, store, buf, metaLookup)
	case "test":
		cmdTest(cfg)
	case "resume":
		cmdResume(cfg, store, os.Args[2:])
	case "pause":
		cmdPause(cfg, store, os.Args[2:])
	case "cleanup":
		cmdCleanup(store, os.Args[2:])
	case "version":
		fmt.Printf("rss-curator v%s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func cmdCheck(cfg models.Config, store *storage.Storage, metaLookup *metadata.Lookup) {
	fmt.Println("Checking RSS feeds...")

	// Set up optional AI support — each subsystem uses its own model config.
	enricherProvider := ai.NewProviderFor("enricher")
	scorerProvider := ai.NewProviderFor("scorer")
	enricher := ai.NewEnricher(enricherProvider, nil)
	scorer := ai.NewScorer(scorerProvider, metaLookup)
	if enricherProvider.Available() || scorerProvider.Available() {
		fmt.Println("AI provider available — enrichment and scoring enabled")
	}

	var m *matcher.Matcher
	if cfg.ShowsConfig != nil {
		m = matcher.NewMatcher(cfg.ShowsConfig, nil)
		fmt.Printf("Using shows.json config (%d shows configured)\n", len(cfg.ShowsConfig.Shows))
	} else {
		m = matcher.NewMatcher(nil, &cfg.MatchRules)
		fmt.Println("Using environment variable config")
	}

	summary, err := ops.RunFeedCheck(context.Background(), ops.FeedCheckConfig{
		Feeds:   cfg.Feeds,
		Matcher: m,
	}, ops.FeedCheckDeps{
		Store:      store,
		Enricher:   enricher,
		Scorer:     scorer,
		ScorerProv: scorerProvider,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	if summary.ItemsFound > 0 {
		fmt.Printf("\n✓ Discovered %d items from RSS feeds\n", summary.ItemsFound)
	}
	if summary.ItemsMatched > 0 {
		fmt.Printf("✓ Staged %d new torrents\n", summary.ItemsMatched)
		fmt.Println("Run 'curator list' to review pending items")
	} else {
		fmt.Println("\nNo new matches found")
	}
	if summary.ItemsScored > 0 {
		fmt.Printf("✓ AI scored/backfilled %d torrents\n", summary.ItemsScored)
	}
}

func cmdList(store *storage.Storage) {
	status := "pending"
	if len(os.Args) > 2 {
		status = os.Args[2]
	}

	torrents, err := store.List(status, "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing torrents: %v\n", err)
		os.Exit(1)
	}

	if len(torrents) == 0 {
		fmt.Printf("No %s torrents\n", status)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tTITLE\tSIZE\tREASON\tDATE\n")
	fmt.Fprintf(w, "--\t-----\t----\t------\t----\n")

	for _, t := range torrents {
		sizeGB := float64(t.FeedItem.Size) / (1024 * 1024 * 1024)
		dateStr := t.StagedAt.Format("Jan 02 15:04")

		// Truncate title if too long
		title := t.FeedItem.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}

		fmt.Fprintf(w, "%d\t%s\t%.2f GB\t%s\t%s\n",
			t.ID, title, sizeGB, t.MatchReason, dateStr)
	}
	w.Flush()
}

func cmdApprove(cfg models.Config, store *storage.Storage, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: curator approve <id> [<id>...]")
		os.Exit(1)
	}

	// Initialize qBittorrent client
	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to qBittorrent: %v\n", err)
		os.Exit(1)
	}

	for _, idStr := range args {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", idStr)
			continue
		}

		torrent, err := store.Get(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting torrent %d: %v\n", id, err)
			continue
		}

		if torrent.Status != "pending" {
			fmt.Printf("Torrent %d already %s\n", id, torrent.Status)
			continue
		}

		// Add to qBittorrent
		fmt.Printf("Adding: %s\n", torrent.FeedItem.Title)
		err = qb.AddTorrent(torrent.FeedItem.Link, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding torrent: %v\n", err)
			continue
		}

		// Update status
		if err := store.UpdateStatus(id, "approved"); err != nil {
			fmt.Fprintf(os.Stderr, "Error updating status: %v\n", err)
			continue
		}

		fmt.Printf("✓ Approved torrent %d\n", id)
	}
}

func cmdReject(store *storage.Storage, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: curator reject <id> [<id>...]")
		os.Exit(1)
	}

	for _, idStr := range args {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", idStr)
			continue
		}

		if err := store.UpdateStatus(id, "rejected"); err != nil {
			fmt.Fprintf(os.Stderr, "Error rejecting torrent %d: %v\n", id, err)
			continue
		}

		fmt.Printf("✓ Rejected torrent %d\n", id)
	}
}

func cmdReview(cfg models.Config, store *storage.Storage) {
	torrents, err := store.List("pending", "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing torrents: %v\n", err)
		os.Exit(1)
	}

	if len(torrents) == 0 {
		fmt.Println("No pending torrents to review")
		return
	}

	// Initialize qBittorrent client
	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to qBittorrent: %v\n", err)
		os.Exit(1)
	}

	for i, t := range torrents {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(torrents), t.FeedItem.Title)
		fmt.Printf("      Size: %.2f GB | Match: %s\n", float64(t.FeedItem.Size)/(1024*1024*1024), t.MatchReason)
		fmt.Printf("      Link: %s\n", t.FeedItem.Link)

		var response string
		fmt.Print("      (a)pprove / (r)eject / (s)kip: ")
		fmt.Scanln(&response)

		response = strings.ToLower(strings.TrimSpace(response))
		switch response {
		case "a", "approve":
			if err := qb.AddTorrent(t.FeedItem.Link, nil); err != nil {
				fmt.Fprintf(os.Stderr, "Error adding torrent: %v\n", err)
			} else {
				store.UpdateStatus(t.ID, "approved")
				fmt.Println("✓ Approved")
			}
		case "r", "reject":
			store.UpdateStatus(t.ID, "rejected")
			fmt.Println("✓ Rejected")
		case "s", "skip":
			fmt.Println("Skipped")
		default:
			fmt.Println("Skipped (invalid input)")
		}
	}

	fmt.Println("\nReview complete!")
}

func cmdTest(cfg models.Config) {
	fmt.Println("Testing connections...")

	// Test qBittorrent
	fmt.Print("qBittorrent... ")
	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Printf("✗ Failed: %v\n", err)
	} else {
		fmt.Println("✓ Connected")

		// Get some info
		torrents, _ := qb.GetTorrents()
		fmt.Printf("  Active torrents: %d\n", len(torrents))
	}

	// Test RSS feeds
	parser := feed.NewParser()
	for i, fc := range cfg.Feeds {
		fmt.Printf("RSS feed %d (%s)... ", i+1, fc.ContentType)
		items, err := parser.Parse(fc.URL, fc.ContentType)
		if err != nil {
			fmt.Printf("✗ Failed: %v\n", err)
		} else {
			fmt.Printf("✓ OK (%d items)\n", len(items))
		}
	}

	// Show config info
	if cfg.ShowsConfig != nil {
		fmt.Printf("\nShows configured: %d\n", len(cfg.ShowsConfig.Shows))
		for _, show := range cfg.ShowsConfig.Shows {
			fmt.Printf("  - %s\n", show.Name)
		}
		fmt.Printf("Movies configured: %d\n", len(cfg.ShowsConfig.Movies))
		for _, movie := range cfg.ShowsConfig.Movies {
			fmt.Printf("  - %s\n", movie.Name)
		}
	}
}

func loadConfig() (models.Config, error) {
	homeDir, _ := os.UserHomeDir()

	cfg := models.Config{
		FeedURLs: []string{
			os.Getenv("RSS_FEED_URL"),
		},
		MovieFeedURLs: []string{
			os.Getenv("RSS_MOVIE_FEED_URL"),
		},
		PollInterval: 30,
		QBittorrent: models.QBConfig{
			Host:      getEnv("QBITTORRENT_HOST", "http://localhost:8080"),
			Username:  os.Getenv("QBITTORRENT_USER"),
			Password:  os.Getenv("QBITTORRENT_PASS"),
			Category:  getEnv("QBITTORRENT_CATEGORY", "curator"),
			SavePath:  getEnv("QBITTORRENT_SAVEPATH", ""),
			AddPaused: getEnv("QBITTORRENT_ADD_PAUSED", "true") == "true",
		},
		MatchRules: models.MatchRule{
			ShowNames:       parseCSV(os.Getenv("SHOW_NAMES")),
			MinQuality:      getEnv("MIN_QUALITY", "1080p"),
			PreferredCodec:  getEnv("PREFERRED_CODEC", "x265"),
			ExcludeGroups:   parseCSV(os.Getenv("EXCLUDE_GROUPS")),
			PreferredGroups: parseCSV(os.Getenv("PREFERRED_GROUPS")),
		},
		StoragePath: getEnv("STORAGE_PATH", filepath.Join(homeDir, ".curator.db")),
	}

	// Log all configured options at startup
	fmt.Println("\n========== Configuration Loaded ==========")
	fmt.Printf("[Config] RSS_FEED_URL: %s\n", cfg.FeedURLs[0])
	fmt.Printf("[Config] RSS_MOVIE_FEED_URL: %s\n", cfg.MovieFeedURLs[0])
	fmt.Printf("[Config] POLL_INTERVAL: %d seconds\n", cfg.PollInterval)
	fmt.Println("\n--- QBittorrent Settings ---")
	fmt.Printf("[Config] QBITTORRENT_HOST: %s\n", cfg.QBittorrent.Host)
	fmt.Printf("[Config] QBITTORRENT_USER: %s\n", cfg.QBittorrent.Username)
	fmt.Printf("[Config] QBITTORRENT_CATEGORY: %s\n", cfg.QBittorrent.Category)
	fmt.Printf("[Config] QBITTORRENT_SAVEPATH: %s\n", cfg.QBittorrent.SavePath)
	fmt.Printf("[Config] QBITTORRENT_ADD_PAUSED (env): %s\n", os.Getenv("QBITTORRENT_ADD_PAUSED"))
	fmt.Printf("[Config] QBITTORRENT_ADD_PAUSED (parsed): %v\n", cfg.QBittorrent.AddPaused)
	fmt.Println("\n--- Match Rules ---")
	fmt.Printf("[Config] SHOW_NAMES: %v\n", cfg.MatchRules.ShowNames)
	fmt.Printf("[Config] MIN_QUALITY: %s\n", cfg.MatchRules.MinQuality)
	fmt.Printf("[Config] PREFERRED_CODEC: %s\n", cfg.MatchRules.PreferredCodec)
	fmt.Printf("[Config] EXCLUDE_GROUPS: %v\n", cfg.MatchRules.ExcludeGroups)
	fmt.Printf("[Config] PREFERRED_GROUPS: %v\n", cfg.MatchRules.PreferredGroups)
	fmt.Println("\n--- Storage ---")
	fmt.Printf("[Config] STORAGE_PATH: %s\n", cfg.StoragePath)
	fmt.Println("==========================================")

	// Validate required fields
	if cfg.FeedURLs[0] == "" {
		return cfg, fmt.Errorf("RSS_FEED_URL environment variable is required")
	}

	// Build combined Feeds slice from FeedURLs + MovieFeedURLs
	for _, u := range cfg.FeedURLs {
		if u != "" {
			cfg.Feeds = append(cfg.Feeds, models.FeedConfig{URL: u, ContentType: models.ContentTypeShow})
		}
	}
	for _, u := range cfg.MovieFeedURLs {
		if u != "" {
			cfg.Feeds = append(cfg.Feeds, models.FeedConfig{URL: u, ContentType: models.ContentTypeMovie})
		}
	}

	// Try to load shows.json
	showsConfig, err := loadShowsConfig()
	if err == nil {
		cfg.ShowsConfig = showsConfig
	} else {
		fmt.Printf("[Config] shows.json not loaded (%v); using environment variable rules fallback\n", err)
	}

	return cfg, nil
}

func loadShowsConfig() (*models.ShowsConfig, error) {
	// Try current directory first
	paths := []string{
		"shows.json",
		filepath.Join(os.Getenv("HOME"), ".curator-shows.json"),
	}

	var data []byte
	var err error

	for _, path := range paths {
		data, err = os.ReadFile(path)
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, err
	}

	var config models.ShowsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse shows.json: %w", err)
	}

	return &config, nil
}

// resolveShowsPath returns the path that should be used as the authoritative
// write target for shows.json. It mirrors the load priority order used by
// loadShowsConfig: if a file already exists at one of the known locations,
// that path is returned. Otherwise "shows.json" (cwd) is the default.
func resolveShowsPath() string {
	candidates := []string{
		"shows.json",
		filepath.Join(os.Getenv("HOME"), ".curator-shows.json"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "shows.json"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

func printUsage() {
	fmt.Print(`rss-curator - RSS feed torrent curator

Usage:
  curator <command> [arguments]

Commands:
  check                Check RSS feeds for new items
  list [status]        List staged torrents (default: pending)
  approve <id>...      Approve and download torrent(s)
  reject <id>...       Reject torrent(s)
  review               Interactive review mode
  test                 Test connections to qBittorrent and RSS feeds
  version              Show version
  resume <id>...       Resume paused torrent(s) in qBittorrent
  pause <id>...        Pause torrent(s) in qBittorrent
  cleanup [pattern]    Remove stale database entries (default: info page links)
  serve                Start API server and scheduler

Configuration:
  1. shows.json (recommended) - Per-show rules
  2. Environment variables (fallback) - See shows.json.sample

Examples:
  curator check                    # Check feeds and stage new matches
  curator list                     # List pending torrents
  curator approve 1 3 5            # Approve specific torrents
  curator review                   # Interactive review mode
  curator cleanup                  # Remove stale info page links
  curator cleanup "%/old/%"        # Remove entries matching pattern
  curator test                     # Test configuration
`)
}

func cmdResume(cfg models.Config, store *storage.Storage, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: curator resume <id> [<id>...]")
		os.Exit(1)
	}

	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to qBittorrent: %v\n", err)
		os.Exit(1)
	}

	for _, idStr := range args {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", idStr)
			continue
		}

		torrent, err := store.GetByID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting torrent %d: %v\n", id, err)
			continue
		}

		if torrent == nil {
			fmt.Fprintf(os.Stderr, "Torrent %d not found\n", id)
			continue
		}

		// Get torrent hash from qBittorrent by matching the download URL
		torrents, err := qb.GetTorrents()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting torrents: %v\n", err)
			continue
		}

		found := false
		for _, t := range torrents {
			// Match by name (title)
			if t.Name == torrent.FeedItem.Title {
				if err := qb.ResumeTorrent(t.Hash); err != nil {
					fmt.Fprintf(os.Stderr, "Error resuming torrent %d: %v\n", id, err)
				} else {
					fmt.Printf("✓ Resumed torrent %d: %s\n", id, torrent.FeedItem.Title)
				}
				found = true
				break
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "Torrent %d not found in qBittorrent\n", id)
		}
	}
}

func cmdPause(cfg models.Config, store *storage.Storage, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: curator pause <id> [<id>...]")
		os.Exit(1)
	}

	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to qBittorrent: %v\n", err)
		os.Exit(1)
	}

	for _, idStr := range args {
		id, err := strconv.Atoi(idStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid ID: %s\n", idStr)
			continue
		}

		torrent, err := store.GetByID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting torrent %d: %v\n", id, err)
			continue
		}

		if torrent == nil {
			fmt.Fprintf(os.Stderr, "Torrent %d not found\n", id)
			continue
		}

		torrents, err := qb.GetTorrents()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting torrents: %v\n", err)
			continue
		}

		found := false
		for _, t := range torrents {
			if t.Name == torrent.FeedItem.Title {
				if err := qb.PauseTorrent(t.Hash); err != nil {
					fmt.Fprintf(os.Stderr, "Error pausing torrent %d: %v\n", id, err)
				} else {
					fmt.Printf("✓ Paused torrent %d: %s\n", id, torrent.FeedItem.Title)
				}
				found = true
				break
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "Torrent %d not found in qBittorrent\n", id)
		}
	}
}

func cmdServe(cfg models.Config, store *storage.Storage, buf *logbuffer.Buffer, metaLookup *metadata.Lookup) {
	// Initialise AI scorer (available even during serve — used for on-demand rescore).
	// Uses CURATOR_AI_SCORER_MODEL if set, falls back to CURATOR_AI_MODEL.
	scorerProvider := ai.NewProviderFor("scorer")
	scorer := ai.NewScorer(scorerProvider, metaLookup)
	enricherProvider := ai.NewProviderFor("enricher")
	enricher := ai.NewEnricher(enricherProvider, nil)
	if scorerProvider.Available() {
		fmt.Println("[Serve] AI scorer provider available — on-demand rescore enabled")
	} else {
		fmt.Println("[Serve] AI scorer provider unavailable — rescore disabled")
	}

	// Suggester uses its own provider/model config so it can be tuned independently.
	suggestProvider := ai.NewProviderFor("suggester")
	if suggestProvider.Available() {
		fmt.Println("[Serve] AI suggester provider available — suggestions enabled")
	} else {
		fmt.Println("[Serve] AI suggester provider unavailable — suggestions disabled (check CURATOR_AI_SUGGESTER_PROVIDER/KEY/HOST)")
	}

	// Create matcher config once for API rematch operations.
	var m *matcher.Matcher
	if cfg.ShowsConfig != nil {
		m = matcher.NewMatcher(cfg.ShowsConfig, nil)
	} else {
		m = matcher.NewMatcher(nil, &cfg.MatchRules)
	}

	// Try to initialize qBittorrent client, but don't fail if unavailable
	var qb *client.Client
	qb, err := client.New(cfg.QBittorrent)
	if err != nil {
		fmt.Printf("[Serve] Warning: qBittorrent unavailable at startup (%v)\n", err)
		fmt.Println("[Serve] API will start but approve/reject operations may fail")
		fmt.Println("[Serve] Retrying qBittorrent connection will happen during operation")
		qb = nil // Set to nil to signal unavailable
	} else {
		fmt.Println("[Serve] qBittorrent connection successful")
	}

	// Parse API port from environment or use default
	port := 8081
	if portStr := os.Getenv("CURATOR_API_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	// Auth configuration
	// Auth is disabled when CURATOR_PASSWORD is unset (local-dev ergonomics preserved).
	authUsername := os.Getenv("CURATOR_USERNAME")
	if authUsername == "" {
		authUsername = "curator"
	}
	authPassword := os.Getenv("CURATOR_PASSWORD")

	sessionSecret := []byte(os.Getenv("CURATOR_SESSION_SECRET"))
	if len(sessionSecret) == 0 {
		b := make([]byte, 32)
		rand.Read(b)
		sessionSecret = b
		if authPassword != "" {
			fmt.Println("[Serve] Warning: CURATOR_SESSION_SECRET not set — sessions will not survive restarts")
		}
	}

	sessionTTLHours := 24
	if ttlStr := os.Getenv("CURATOR_SESSION_TTL_HOURS"); ttlStr != "" {
		if h, err := strconv.Atoi(ttlStr); err == nil && h > 0 {
			sessionTTLHours = h
		}
	}

	auth := api.AuthConfig{
		Username:      authUsername,
		Password:      authPassword,
		SessionSecret: sessionSecret,
		SessionTTL:    time.Duration(sessionTTLHours) * time.Hour,
	}

	// Create and start API server (even if qBittorrent is unavailable)

	// Recover from any previous unclean shutdown: mark orphaned 'running' jobs
	// as failed so the UI does not show ghost jobs indefinitely.
	if n, err := store.MarkStaleJobsFailed("Interrupted: server was restarted"); err != nil {
		fmt.Fprintf(os.Stderr, "[Serve] Warning: stale job recovery failed: %v\n", err)
	} else if n > 0 {
		fmt.Printf("[Serve] Recovered %d stale job(s) from previous crash\n", n)
	}

	// Job queue — single worker, used for on-demand async operations.
	q := jobs.New(nil)
	q.Start()

	// Scheduler — periodic feed_check task replacing the external scheduler.sh cron.
	feedCheckInterval := 3600 * time.Second
	if v := os.Getenv("CHECK_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			feedCheckInterval = time.Duration(n) * time.Second
		}
	}
	// rescore_backfill runs on its own cadence; defaults to the feed-check
	// interval but can be tuned independently.
	backfillInterval := feedCheckInterval
	if v := os.Getenv("CURATOR_AI_BACKFILL_INTERVAL_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			backfillInterval = time.Duration(n) * time.Second
		}
	}
	feedCheckCfg := ops.FeedCheckConfig{
		Feeds:   cfg.Feeds,
		Matcher: m,
	}
	feedCheckDeps := ops.FeedCheckDeps{
		Store:      store,
		Enricher:   enricher,
		Scorer:     scorer,
		ScorerProv: scorerProvider,
		LogBuffer:  buf,
	}
	sched := scheduler.New()
	sched.Register(&scheduler.Task{
		Type:     "feed_check",
		Interval: feedCheckInterval,
		Enabled:  true,
		Fn: func(ctx context.Context) {
			ops.RunFeedCheck(ctx, feedCheckCfg, feedCheckDeps)
		},
	})

	// suggest_refresh — daily background suggestion cache rebuild.
	sg := suggester.New(store, suggestProvider, m, metaLookup)
	suggestRefreshInterval := 24 * time.Hour
	if v := os.Getenv("CURATOR_AI_SUGGESTER_REFRESH_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			suggestRefreshInterval = time.Duration(n) * time.Hour
		}
	}
	sched.Register(&scheduler.Task{
		Type:     "suggest_refresh",
		Interval: suggestRefreshInterval,
		Enabled:  suggestProvider.Available(),
		Fn: func(ctx context.Context) {
			jobID, err := store.CreateJob("suggest_refresh")
			if err != nil {
				return
			}
			if err := sg.RefreshCache(ctx); err != nil {
				_ = store.FailJob(jobID, err.Error())
				return
			}
			_ = store.CompleteJob(jobID, models.JobSummary{})
		},
	})

	// rescore_backfill — score items ingested while the AI provider was down.
	// Initially gated on provider availability; the in-app toggle is applied
	// after settingsMgr loads (see SetEnabled call below).
	backfillDeps := ops.RescoreBackfillDeps{
		Store:      store,
		Scorer:     scorer,
		ScorerProv: scorerProvider,
		LogBuffer:  buf,
	}
	sched.Register(&scheduler.Task{
		Type:     "rescore_backfill",
		Interval: backfillInterval,
		Enabled:  scorerProvider.Available(),
		Fn: func(ctx context.Context) {
			ops.RunRescoreBackfill(ctx, backfillDeps)
		},
	})

	sched.Start()

	// Cold-cache fill: if suggestions cache is empty and provider is available,
	// trigger an immediate background refresh so the UI has results on first open.
	if suggestProvider.Available() {
		if raw, _, _ := store.GetCachedSuggestions(); raw == nil {
			sched.RunNow("suggest_refresh")
		}
	}

	// Settings manager — load DB overrides on top of env-var defaults.
	// Placed after feedCheckInterval so EnvDefaults can capture it.
	settingsMgr := settings.NewManager(store)
	progressIntervalEnv := 0
	if v := os.Getenv("CURATOR_PROGRESS_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			progressIntervalEnv = n
		}
	}
	envDefaults := settings.EnvDefaults{
		FeedCheckIntervalSecs: int(feedCheckInterval.Seconds()),
		ProgressInterval:      progressIntervalEnv,
		MinQuality:            cfg.MatchRules.MinQuality,
		PreferredCodec:        cfg.MatchRules.PreferredCodec,
		ExcludeGroups:         cfg.MatchRules.ExcludeGroups,
		PreferredGroups:       cfg.MatchRules.PreferredGroups,
		AuthUsername:          authUsername,
		AuthPassword:          authPassword,
	}
	if err := settingsMgr.Load(envDefaults); err != nil {
		fmt.Fprintf(os.Stderr, "[Serve] Warning: could not load settings from DB: %v\n", err)
	}
	// Apply the DB-backed enabled state for rescore_backfill now that settings
	// are loaded. Subsequent changes via the UI call SetEnabled through the
	// settings save handler in server.go.
	sched.SetEnabled("rescore_backfill",
		scorerProvider.Available() && settingsMgr.Get().Scheduler.RescoreBackfillEnabled)

	// On-demand feed-check: backfill suppressed (BackfillEnabled stays nil in the
	// stored deps — the handler overrides it to false before each submission).
	onDemandFeedCheckDeps := feedCheckDeps

	server := api.NewServer(store, qb, port, buf, scorer, scorerProvider, m, enricher, auth).
		WithScheduler(sched).
		WithQueue(q).
		WithSettings(settingsMgr).
		WithShowsPath(resolveShowsPath()).
		WithSuggester(sg).
		WithFeedCheck(feedCheckCfg, onDemandFeedCheckDeps)
	fmt.Printf("[Serve] Starting API server on port %d\n", port)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting API server: %v\n", err)
		os.Exit(1)
	}

	// Block until SIGTERM or SIGINT, then drain components in order.
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	<-sigCtx.Done()
	fmt.Println("[Serve] Shutdown signal received, draining…")

	const drainTimeout = 30 * time.Second

	// 1. Drain in-flight HTTP requests.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), drainTimeout)
	defer shutCancel()
	if err := server.Shutdown(shutCtx); err != nil {
		fmt.Fprintf(os.Stderr, "[Serve] HTTP shutdown error: %v\n", err)
	}

	// 2. Stop the scheduler (has its own internal 30s task-drain timeout).
	sched.Stop()

	// 3. Stop the job queue — waits for any in-flight job; discards queued items.
	q.Stop()

	// 4. Close the database.
	if err := store.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "[Serve] DB close error: %v\n", err)
	}

	fmt.Println("[Serve] Shutdown complete.")
}

func cmdCleanup(store *storage.Storage, args []string) {
	if len(args) == 0 {
		// Default: clean up info page links (IPTorrents /t/{id} format)
		fmt.Println("Cleaning up stale torrent links...")
		fmt.Println("Removing pending entries with info page links (e.g., /t/{id})...")

		patterns := []string{"%/t/%"}
		deleted, err := store.CleanupStaleLinks(patterns)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error cleaning up: %v\n", err)
			os.Exit(1)
		}

		if deleted == 0 {
			fmt.Println("✓ No stale entries found")
		} else {
			fmt.Printf("✓ Removed %d stale torrent entries\n", deleted)
		}
		return
	}

	// Custom cleanup: accept patterns as arguments
	fmt.Printf("Cleaning up entries matching pattern(s): %v\n", args)
	deleted, err := store.CleanupStaleLinks(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error cleaning up: %v\n", err)
		os.Exit(1)
	}

	if deleted == 0 {
		fmt.Println("✓ No matching entries found")
	} else {
		fmt.Printf("✓ Removed %d torrent entries\n", deleted)
	}
}
