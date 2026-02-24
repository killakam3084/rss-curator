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
	qbClient := qbt.NewClient(qbt.Config{
		Host:     cfg.Host,
		Username: cfg.Username,
		Password: cfg.Password,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := qbClient.GetTorrentsCtx(ctx, qbt.TorrentFilterOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to qBittorrent: %w", err)
	}

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

	err := c.qb.AddTorrentFromUrlCtx(ctx, url, opts)
	if err != nil {
		return fmt.Errorf("failed to add torrent: %w", err)
	}

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
