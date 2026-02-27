package client

import (
	"context"
	"fmt"
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
	fmt.Printf("[QBittorrent] c.addPaused value: %v\n", c.addPaused)
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

	fmt.Printf("[QBittorrent] Final options before AddTorrentFromUrlCtx: %v\n", opts)
	fmt.Printf("[QBittorrent] Adding torrent from URL: %s with options: %v\n", url, opts)

	// Single attempt - no automatic retries
	err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
	if err != nil {
		fmt.Printf("[QBittorrent] Failed to add torrent: error_type=%T\n", err)
		fmt.Printf("[QBittorrent] Error: %v\n", extractErrorDetails(err))
		fmt.Printf("[QBittorrent] Raw error: %+v\n", err)
		fmt.Printf("[QBittorrent] DEBUG: If error mentions 'bencoded', the URL may not be serving a valid .torrent file\n")
		fmt.Printf("[QBittorrent] DEBUG: Possible causes:\n")
		fmt.Printf("[QBittorrent]   - Wrong URL format\n")
		fmt.Printf("[QBittorrent]   - Missing authentication cookies (site requires login)\n")
		fmt.Printf("[QBittorrent]   - Rate limiting or blocked request\n")
		fmt.Printf("[QBittorrent]   - Site is serving redirect/error page instead of .torrent file\n")
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
	fmt.Printf("[QBittorrent] Retry: Adding torrent from URL: %s\n", url)

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

		fmt.Printf("[QBittorrent] Retry attempt %d: Adding torrent from URL: %s\n", attempt+1, url)
		err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
		if err == nil {
			if attempt > 0 {
				fmt.Printf("[QBittorrent] Successfully added torrent on retry attempt %d\n", attempt)
			}
			return nil
		}

		lastErr = err
		fmt.Printf("[QBittorrent] Retry attempt %d failed: %s\n", attempt+1, extractErrorDetails(err))
		fmt.Printf("[QBittorrent] NOTE: If error is 'bencoded string', the URL may be serving HTML instead of .torrent file\n")
		fmt.Printf("[QBittorrent] This typically means: wrong URL format, site requires authentication, or rate limiting\n")
		fmt.Printf("[QBittorrent] SOLUTION: Ensure qBittorrent has authentication cookies for restricted sites\n")
	}

	return lastErr
}

// Helper functions for URL validation

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

// extractErrorDetails provides detailed error information for debugging qBittorrent API issues
func extractErrorDetails(err error) string {
	if err == nil {
		return "no error"
	}

	// Check for common error types and HTTP status codes
	errStr := err.Error()

	// Try to detect HTTP status codes from error message
	if strings.Contains(errStr, "401") || strings.Contains(errStr, "Unauthorized") {
		return "HTTP 401 Unauthorized - check qBittorrent credentials"
	}
	if strings.Contains(errStr, "403") || strings.Contains(errStr, "Forbidden") {
		return "HTTP 403 Forbidden - qBittorrent may have rejected the request"
	}
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "Not Found") {
		return "HTTP 404 Not Found - check qBittorrent host and port"
	}
	if strings.Contains(errStr, "400") || strings.Contains(errStr, "Bad Request") {
		return "HTTP 400 Bad Request - qBittorrent rejected the URL or options"
	}
	if strings.Contains(errStr, "409") {
		return "HTTP 409 Conflict - torrent may already exist in qBittorrent"
	}
	if strings.Contains(errStr, "415") {
		return "HTTP 415 Unsupported Media Type - qBittorrent doesn't recognize torrent format"
	}
	if strings.Contains(errStr, "connection refused") {
		return "Connection refused - qBittorrent may not be running or port is wrong"
	}
	if strings.Contains(errStr, "timeout") {
		return "Connection timeout - qBittorrent is not responding"
	}
	if strings.Contains(errStr, "TooManyRequests") || strings.Contains(errStr, "429") {
		return "HTTP 429 Too Many Requests - qBittorrent rate limiting"
	}
	if strings.Contains(errStr, "bencoded") {
		return "Bencoded error - Downloaded content is not a valid torrent file. Likely causes: missing authentication cookies, site redirect, or HTML error page"
	}

	// Return the original error message if we can't identify it
	return fmt.Sprintf("Unknown error: %s", errStr)
}
