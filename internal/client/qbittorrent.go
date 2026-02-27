package client

import (
	"context"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/iillmaticc/rss-curator/pkg/models"
)

// Client wraps qBittorrent client
type Client struct {
	qb        *qbt.Client
	category  string
	savePath  string
	addPaused bool
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
		qb:        qbClient,
		category:  cfg.Category,
		savePath:  cfg.SavePath,
		addPaused: cfg.AddPaused,
	}, nil
}

// AddTorrent adds a torrent by URL
func (c *Client) AddTorrent(url string, options map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	// Override with any provided options
	for k, v := range options {
		opts[k] = v
	}

	fmt.Printf("[QBittorrent] Adding torrent from URL: %s with options: %v\n", url, opts)
	err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
	if err != nil {
		fmt.Printf("[QBittorrent] Failed to add torrent: %v\n", err)
		return fmt.Errorf("failed to add torrent: %w", err)
	}

	fmt.Printf("[QBittorrent] Successfully added torrent from URL: %s\n", url)
	return nil
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
