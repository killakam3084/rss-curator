package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
)

func setupTestDB(t *testing.T) (*Storage, string) {
	tmpDir, err := os.MkdirTemp("", "rss-curator-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	return store, tmpDir
}

func cleanupTestDB(store *Storage, tmpDir string) {
	store.Close()
	os.RemoveAll(tmpDir)
}

func createTestTorrent() models.StagedTorrent {
	return models.StagedTorrent{
		FeedItem: models.FeedItem{
			Title:       "Test Torrent",
			Link:        "http://example.com/test.torrent",
			GUID:        "http://example.com/test-1",
			PubDate:     time.Now(),
			Size:        1024 * 1024 * 100,
			Description: "Test torrent description",
		},
		MatchReason: "Test match rule",
		StagedAt:    time.Now(),
		Status:      "pending",
	}
}

func TestAddTorrent(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent := createTestTorrent()
	err := store.Add(torrent)
	if err != nil {
		t.Fatalf("failed to add torrent: %v", err)
	}
}

func TestGetTorrent(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent := createTestTorrent()
	err := store.Add(torrent)
	if err != nil {
		t.Fatalf("failed to add torrent: %v", err)
	}

	retrieved, err := store.Get(1)
	if err != nil {
		t.Fatalf("failed to get torrent: %v", err)
	}

	if retrieved == nil {
		t.Fatalf("expected torrent, got nil")
	}

	if retrieved.FeedItem.Title != torrent.FeedItem.Title {
		t.Errorf("expected title '%s', got '%s'", torrent.FeedItem.Title, retrieved.FeedItem.Title)
	}

	if retrieved.Status != "pending" {
		t.Errorf("expected status 'pending', got '%s'", retrieved.Status)
	}
}

func TestUpdateStatus(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent := createTestTorrent()
	store.Add(torrent)

	err := store.UpdateStatus(1, "accepted")
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	updated, err := store.Get(1)
	if err != nil {
		t.Fatalf("failed to get updated torrent: %v", err)
	}

	if updated.Status != "accepted" {
		t.Errorf("expected status 'accepted', got '%s'", updated.Status)
	}
}

func TestLogActivity(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent := createTestTorrent()
	store.Add(torrent)

	err := store.LogActivity(1, torrent.FeedItem.Title, "approve", torrent.MatchReason)
	if err != nil {
		t.Fatalf("failed to log activity: %v", err)
	}

	count, err := store.GetActivityCount("approve")
	if err != nil {
		t.Fatalf("failed to get activity count: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 activity, got %d", count)
	}
}

func TestListTorrents(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	torrent1 := createTestTorrent()
	torrent1.FeedItem.Link = "http://example.com/test1.torrent"
	torrent1.FeedItem.GUID = "http://example.com/test-1"
	store.Add(torrent1)

	torrent2 := createTestTorrent()
	torrent2.FeedItem.Link = "http://example.com/test2.torrent"
	torrent2.FeedItem.GUID = "http://example.com/test-2"
	torrent2.Status = "accepted"
	store.Add(torrent2)

	pending, err := store.List("pending")
	if err != nil {
		t.Fatalf("failed to list pending: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("expected 1 pending torrent, got %d", len(pending))
	}
}
