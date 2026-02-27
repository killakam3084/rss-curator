package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/iillmaticc/rss-curator/internal/api"
	"github.com/iillmaticc/rss-curator/internal/client"
	"github.com/iillmaticc/rss-curator/internal/feed"
	"github.com/iillmaticc/rss-curator/internal/matcher"
	"github.com/iillmaticc/rss-curator/internal/storage"
	"github.com/iillmaticc/rss-curator/pkg/models"
)

const (
	version = "0.5.1"
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

	switch command {
	case "check", "scan":
		cmdCheck(cfg, store)
	case "list", "ls":
		cmdList(store)
	case "approve":
		cmdApprove(cfg, store, os.Args[2:])
	case "reject":
		cmdReject(store, os.Args[2:])
	case "review":
		cmdReview(cfg, store)
	case "serve":
		cmdServe(cfg, store)
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

func cmdCheck(cfg models.Config, store *storage.Storage) {
	fmt.Println("Checking RSS feeds...")

	parser := feed.NewParser()

	// Create matcher with shows config or legacy rules
	var m *matcher.Matcher
	if cfg.ShowsConfig != nil {
		m = matcher.NewMatcher(cfg.ShowsConfig, nil)
		fmt.Printf("Using shows.json config (%d shows configured)\n", len(cfg.ShowsConfig.Shows))
	} else {
		m = matcher.NewMatcher(nil, &cfg.MatchRules)
		fmt.Println("Using environment variable config")
	}

	totalNew := 0
	for _, feedURL := range cfg.FeedURLs {
		fmt.Printf("Fetching: %s\n", feedURL)

		items, err := parser.Parse(feedURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing feed: %v\n", err)
			continue
		}

		fmt.Printf("Found %d items\n", len(items))

		// Match items
		matches := m.MatchAll(items)
		fmt.Printf("Matched %d items\n", len(matches))

		// Stage matches
		for _, match := range matches {
			if err := store.Add(match); err != nil {
				fmt.Fprintf(os.Stderr, "Error staging torrent: %v\n", err)
			} else {
				totalNew++
			}
		}
	}

	if totalNew > 0 {
		fmt.Printf("\n✓ Staged %d new torrents\n", totalNew)
		fmt.Println("Run 'curator list' to review pending items")
	} else {
		fmt.Println("\nNo new matches found")
	}
}

func cmdList(store *storage.Storage) {
	status := "pending"
	if len(os.Args) > 2 {
		status = os.Args[2]
	}

	torrents, err := store.List(status)
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
	torrents, err := store.List("pending")
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
	for i, feedURL := range cfg.FeedURLs {
		fmt.Printf("RSS feed %d... ", i+1)
		items, err := parser.Parse(feedURL)
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
	}
}

func loadConfig() (models.Config, error) {
	homeDir, _ := os.UserHomeDir()

	cfg := models.Config{
		FeedURLs: []string{
			os.Getenv("RSS_FEED_URL"),
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
			ShowNames:       strings.Split(os.Getenv("SHOW_NAMES"), ","),
			MinQuality:      getEnv("MIN_QUALITY", "1080p"),
			PreferredCodec:  getEnv("PREFERRED_CODEC", "x265"),
			ExcludeGroups:   strings.Split(os.Getenv("EXCLUDE_GROUPS"), ","),
			PreferredGroups: strings.Split(os.Getenv("PREFERRED_GROUPS"), ","),
		},
		StoragePath: getEnv("STORAGE_PATH", filepath.Join(homeDir, ".curator.db")),
	}

	// Log all configured options at startup
	fmt.Println("\n========== Configuration Loaded ==========")
	fmt.Printf("[Config] RSS_FEED_URL: %s\n", cfg.FeedURLs[0])
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
	fmt.Println("==========================================\n")

	// Validate required fields
	if cfg.FeedURLs[0] == "" {
		return cfg, fmt.Errorf("RSS_FEED_URL environment variable is required")
	}

	// Try to load shows.json
	showsConfig, err := loadShowsConfig()
	if err == nil {
		cfg.ShowsConfig = showsConfig
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func printUsage() {
	fmt.Println(`rss-curator - RSS feed torrent curator

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

func cmdServe(cfg models.Config, store *storage.Storage) {
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

	// Create and start API server (even if qBittorrent is unavailable)
	server := api.NewServer(store, qb, port)
	fmt.Printf("[Serve] Starting API server on port %d\n", port)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting API server: %v\n", err)
		os.Exit(1)
	}
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
