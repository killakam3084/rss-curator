package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/iillmaticc/rss-curator/pkg/models"
)

// Client wraps qBittorrent client
type Client struct {
	qb           *qbt.Client
	category     string
	savePath     string
	addPaused    bool
	maxRetries   int
	retryDelayMs int // Initial delay in milliseconds between retries
}

// New creates a new qBittorrent client
func New(cfg models.QBConfig) (*Client, error) {
	fmt.Printf("[QBittorrent] Initializing client - Host: %s, Username: %s\n", cfg.Host, cfg.Username)

	qbClient := qbt.NewClient(qbt.Config{
		Host:     cfg.Host,
		Username: cfg.Username,
		Password: cfg.Password,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Printf("[QBittorrent] Testing connection to %s...\n", cfg.Host)
	_, err := qbClient.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err != nil {
		fmt.Printf("[QBittorrent] Connection failed: %v\n", err)
		return nil, fmt.Errorf("failed to connect to qBittorrent at %s: %w", cfg.Host, err)
	}

	fmt.Printf("[QBittorrent] Connection successful to %s\n", cfg.Host)
	return &Client{
		qb:           qbClient,
		category:     cfg.Category,
		savePath:     cfg.SavePath,
		addPaused:    cfg.AddPaused,
		maxRetries:   3,   // Retry up to 3 times
		retryDelayMs: 500, // Start with 500ms delay, exponential backoff
	}, nil
}

// AddTorrent adds a torrent by URL
func (c *Client) AddTorrent(url string, options map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Validate URL format
	if url == "" {
		return fmt.Errorf("torrent URL cannot be empty")
	}

	// Extract title from options if provided (for URL transformation)
	title := ""
	if options != nil {
		title = options["title"]
	}

	// Transform info page URLs to actual torrent download URLs
	originalURL := url
	url = transformTorrentURL(url, title)
	if url != originalURL {
		fmt.Printf("[QBittorrent] Transformed URL from: %s\n", originalURL)
		fmt.Printf("[QBittorrent] Transformed URL to: %s\n", url)
	}

	// Log what we received
	fmt.Printf("[QBittorrent] Received URL to add: %s\n", url)
	fmt.Printf("[QBittorrent] URL scheme: %v (is magnet: %v, is http: %v)\n",
		getURLScheme(url),
		isMagnetLink(url),
		isHTTPURL(url))

	// Merge default options with any provided options
	opts := map[string]string{
		"category": c.category,
	}
	if c.savePath != "" {
		opts["savepath"] = c.savePath
	}

	// Set paused state based on client config (can be overridden by options)
	if c.addPaused {
		opts["paused"] = "true"
	} else {
		opts["paused"] = "false"
	}

	// Override with any provided options (except "title" which is just for transformation)
	if options != nil {
		for k, v := range options {
			if k != "title" { // Skip the title key as it's only for URL transformation
				opts[k] = v
			}
		}
	}

	fmt.Printf("[QBittorrent] Adding torrent from URL: %s with options: %v\n", url, opts)

	// Single attempt - no automatic retries
	err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
	if err != nil {
		fmt.Printf("[QBittorrent] Failed to add torrent: %v\n", err)
		return fmt.Errorf("failed to add torrent: %w", err)
	}

	fmt.Printf("[QBittorrent] Successfully added torrent from URL: %s\n", url)

	// Log current torrents to verify addition
	torrents, err := c.GetTorrents()
	if err == nil {
		fmt.Printf("[QBittorrent] Current torrent count after add: %d\n", len(torrents))
	}

	return nil
}

// RetryAddTorrent attempts to add a torrent with exponential backoff retry logic
// This is designed for manual retries from the UI when initial add fails
func (c *Client) RetryAddTorrent(ctx context.Context, url string, opts map[string]string) error {
	var lastErr error
	delayMs := c.retryDelayMs

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("[QBittorrent] Retry attempt %d/%d after %dms delay...\n", attempt, c.maxRetries, delayMs)
			// Check context before sleeping
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(delayMs) * time.Millisecond):
				// Continue with retry
			}
			// Exponential backoff: double the delay for next retry (capped at 5 seconds)
			delayMs = delayMs * 2
			if delayMs > 5000 {
				delayMs = 5000
			}
		}

		err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
		if err == nil {
			if attempt > 0 {
				fmt.Printf("[QBittorrent] Successfully added torrent on retry attempt %d\n", attempt)
			}
			return nil
		}

		lastErr = err
		fmt.Printf("[QBittorrent] Attempt %d failed: %v\n", attempt+1, err)
	}

	return lastErr
}

// Helper functions for URL validation and transformation

// transformTorrentURL converts info page links to actual .torrent download URLs
// Example: https://iptorrents.com/t/7228493 → https://iptorrents.com/download.php/7228493/{title}.torrent
// Takes optional title parameter to construct proper .torrent filename
func transformTorrentURL(url_str, title string) string {
	// IPTorrents: https://iptorrents.com/t/7228493 -> https://iptorrents.com/download.php/7228493/{filename}.torrent
	if strings.Contains(url_str, "iptorrents.com/t/") {
		// Extract the torrent ID from /t/{id}
		parts := strings.Split(url_str, "/t/")
		if len(parts) == 2 {
			torrentID := parts[1]
			// Clean up torrent ID (remove trailing slashes, query params, fragments)
			torrentID = strings.TrimRight(torrentID, "/")
			if idx := strings.Index(torrentID, "?"); idx > -1 {
				torrentID = torrentID[:idx]
			}
			if idx := strings.Index(torrentID, "#"); idx > -1 {
				torrentID = torrentID[:idx]
			}

			// Build filename from title if provided, otherwise use generic name
			filename := "torrent.torrent"
			if title != "" {
				// Clean and encode title for use as filename
				cleanTitle := strings.TrimSpace(title)
				// URL encode the title (spaces become %20, etc.)
				cleanTitle = url.QueryEscape(cleanTitle)
				filename = cleanTitle + ".torrent"
			}

			return "https://iptorrents.com/download.php/" + torrentID + "/" + filename
		}
	}

	// Add more torrent site transformations here as needed
	// For now, return the URL unchanged if no transformation matches
	return url_str
}

func isMagnetLink(url string) bool {
	return strings.HasPrefix(url, "magnet:")
}

func isHTTPURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func getURLScheme(url string) string {
	if strings.HasPrefix(url, "magnet:") {
		return "magnet"
	}
	if strings.HasPrefix(url, "https://") {
		return "https"
	}
	if strings.HasPrefix(url, "http://") {
		return "http"
	}
	return "unknown"
}

// GetTorrents retrieves current torrents
func (c *Client) GetTorrents() ([]qbt.Torrent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	torrents, err := c.qb.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	return torrents, nil
}

// GetCategories retrieves available categories
func (c *Client) GetCategories() (map[string]qbt.Category, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.qb.GetCategoriesCtx(ctx)
}

// CreateCategory creates a new category
func (c *Client) CreateCategory(name, path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.qb.CreateCategoryCtx(ctx, name, path)
}

// ResumeTorrent resumes/starts a paused torrent
func (c *Client) ResumeTorrent(hash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.qb.ResumeCtx(ctx, []string{hash})
}

// PauseTorrent pauses a torrent
func (c *Client) PauseTorrent(hash string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return c.qb.PauseCtx(ctx, []string{hash})
}

// TestConnection tests the connection to qBittorrent
func (c *Client) TestConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.qb.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	return err
}
