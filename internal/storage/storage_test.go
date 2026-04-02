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

	pending, err := store.List("pending", "")
	if err != nil {
		t.Fatalf("failed to list pending: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("expected 1 pending torrent, got %d", len(pending))
	}
}

func TestListByQuery(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	t1 := createTestTorrent()
	t1.FeedItem.Title = "Breaking Bad S01E01"
	t1.FeedItem.GUID = "guid-bb-1"
	t1.FeedItem.Link = "http://example.com/bb.torrent"
	store.Add(t1)

	t2 := createTestTorrent()
	t2.FeedItem.Title = "Better Call Saul S01E01"
	t2.FeedItem.GUID = "guid-bcs-1"
	t2.FeedItem.Link = "http://example.com/bcs.torrent"
	store.Add(t2)

	results, err := store.List("", "Breaking Bad")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FeedItem.Title != "Breaking Bad S01E01" {
		t.Errorf("unexpected title: %q", results[0].FeedItem.Title)
	}
}

func TestListByQueryNoMatch(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	t1 := createTestTorrent()
	t1.FeedItem.GUID = "guid-nm-1"
	t1.FeedItem.Link = "http://example.com/nm.torrent"
	store.Add(t1)

	results, err := store.List("", "zzz-no-match")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestListByStatusAndQuery(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	t1 := createTestTorrent()
	t1.FeedItem.Title = "Sopranos S01E01"
	t1.FeedItem.GUID = "guid-sop-1"
	t1.FeedItem.Link = "http://example.com/sop1.torrent"
	// status = "pending" (default)
	store.Add(t1)

	t2 := createTestTorrent()
	t2.FeedItem.Title = "Sopranos S01E02"
	t2.FeedItem.GUID = "guid-sop-2"
	t2.FeedItem.Link = "http://example.com/sop2.torrent"
	t2.Status = "rejected"
	store.Add(t2)

	results, err := store.List("rejected", "Sopranos")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FeedItem.Title != "Sopranos S01E02" {
		t.Errorf("unexpected title: %q", results[0].FeedItem.Title)
	}
	if results[0].Status != "rejected" {
		t.Errorf("expected status 'rejected', got %q", results[0].Status)
	}
}

// ── Job CRUD tests ────────────────────────────────────────────────────────────

func TestCreateJob(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	id, err := store.CreateJob("feed_check")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive job ID, got %d", id)
	}

	job, err := store.GetJob(id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job == nil {
		t.Fatal("GetJob returned nil for existing job")
	}
	if job.Type != "feed_check" {
		t.Errorf("expected type 'feed_check', got %q", job.Type)
	}
	if job.Status != "running" {
		t.Errorf("expected status 'running', got %q", job.Status)
	}
	if job.CompletedAt != nil {
		t.Error("completed_at should be nil for a running job")
	}
}

func TestCompleteJob(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	id, _ := store.CreateJob("rescore")
	summary := models.JobSummary{
		ItemsFound:   10,
		ItemsMatched: 3,
		ItemsScored:  3,
	}

	if err := store.CompleteJob(id, summary); err != nil {
		t.Fatalf("CompleteJob: %v", err)
	}

	job, err := store.GetJob(id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", job.Status)
	}
	if job.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if job.Summary.ItemsFound != 10 {
		t.Errorf("expected items_found=10, got %d", job.Summary.ItemsFound)
	}
}

func TestFailJob(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	id, _ := store.CreateJob("feed_check")
	if err := store.FailJob(id, "network timeout"); err != nil {
		t.Fatalf("FailJob: %v", err)
	}

	job, err := store.GetJob(id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", job.Status)
	}
	if job.Summary.ErrorMessage != "network timeout" {
		t.Errorf("expected error_message 'network timeout', got %q", job.Summary.ErrorMessage)
	}
}

func TestCancelJob(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	id, _ := store.CreateJob("rematch")
	summary := models.JobSummary{ItemsFound: 100, ItemsMatched: 42, ItemsScored: 9}
	if err := store.CancelJob(id, summary); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}

	job, err := store.GetJob(id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != "cancelled" {
		t.Errorf("expected status 'cancelled', got %q", job.Status)
	}
	if job.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
	if job.Summary.ItemsMatched != 42 {
		t.Errorf("expected items_matched=42, got %d", job.Summary.ItemsMatched)
	}
}

func TestListJobs(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	// Create 3 jobs: 2 completed, 1 failed
	id1, _ := store.CreateJob("feed_check")
	id2, _ := store.CreateJob("rescore")
	id3, _ := store.CreateJob("feed_check")

	_ = store.CompleteJob(id1, models.JobSummary{ItemsFound: 5})
	_ = store.CompleteJob(id2, models.JobSummary{ItemsScored: 2})
	_ = store.FailJob(id3, "timeout")

	// Wait a tiny bit so timestamps don't collide
	time.Sleep(time.Millisecond)

	all, err := store.ListJobs(10, "")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(all))
	}

	failed, err := store.ListJobs(10, "failed")
	if err != nil {
		t.Fatalf("ListJobs(failed): %v", err)
	}
	if len(failed) != 1 {
		t.Errorf("expected 1 failed job, got %d", len(failed))
	}

	completed, err := store.ListJobs(10, "completed")
	if err != nil {
		t.Fatalf("ListJobs(completed): %v", err)
	}
	if len(completed) != 2 {
		t.Errorf("expected 2 completed jobs, got %d", len(completed))
	}

	id4, _ := store.CreateJob("rematch")
	_ = store.CancelJob(id4, models.JobSummary{ItemsFound: 7, ItemsMatched: 3})

	cancelled, err := store.ListJobs(10, "cancelled")
	if err != nil {
		t.Fatalf("ListJobs(cancelled): %v", err)
	}
	if len(cancelled) != 1 {
		t.Errorf("expected 1 cancelled job, got %d", len(cancelled))
	}
}

func TestGetJob_NotFound(t *testing.T) {
	store, tmpDir := setupTestDB(t)
	defer cleanupTestDB(store, tmpDir)

	job, err := store.GetJob(9999)
	if err != nil {
		t.Fatalf("GetJob on missing ID returned error: %v", err)
	}
	if job != nil {
		t.Error("expected nil for missing job, got non-nil")
	}
}
