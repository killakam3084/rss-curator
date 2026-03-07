package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
)

// mockStorage implements the Storage interface for testing
type mockStorage struct {
	torrents   map[int]*models.StagedTorrent
	activities []models.Activity
}

// Get returns a torrent by ID
func (m *mockStorage) Get(id int) (*models.StagedTorrent, error) {
	return m.torrents[id], nil
}

// List returns torrents by status
func (m *mockStorage) List(status string) ([]models.StagedTorrent, error) {
	var result []models.StagedTorrent
	for _, t := range m.torrents {
		if status == "" || t.Status == status {
			result = append(result, *t)
		}
	}
	return result, nil
}

// Add adds a torrent
func (m *mockStorage) Add(torrent models.StagedTorrent) error {
	return nil
}

// UpdateStatus updates a torrent's status
func (m *mockStorage) UpdateStatus(id int, status string) error {
	if t, ok := m.torrents[id]; ok {
		t.Status = status
		return nil
	}
	return nil
}

// LogActivity logs an activity
func (m *mockStorage) LogActivity(torrentID int, title, action, matchReason string) error {
	m.activities = append(m.activities, models.Activity{
		TorrentID:    torrentID,
		TorrentTitle: title,
		Action:       action,
		ActionAt:     time.Now(),
		MatchReason:  matchReason,
	})
	return nil
}

// GetActivity returns activities
func (m *mockStorage) GetActivity(limit int, offset int, action string) ([]models.Activity, error) {
	// Stub implementation
	return []models.Activity{}, nil
}

// GetActivityCount returns activity count
func (m *mockStorage) GetActivityCount(action string) (int, error) {
	count := 0
	for _, a := range m.activities {
		if action == "" || a.Action == action {
			count++
		}
	}
	return count, nil
}

// DeleteOld deletes old entries
func (m *mockStorage) DeleteOld(olderThan time.Duration) error {
	return nil
}

// CleanupStaleLinks cleans up stale links
func (m *mockStorage) CleanupStaleLinks(patterns []string) (int64, error) {
	return 0, nil
}

// Close closes the storage
func (m *mockStorage) Close() error {
	return nil
}

// GetByID returns a torrent by ID
func (m *mockStorage) GetByID(id int) (*models.StagedTorrent, error) {
	return m.torrents[id], nil
}

// AddRawFeedItem adds a raw feed item
func (m *mockStorage) AddRawFeedItem(item models.RawFeedItem) error {
	return nil
}

// GetRawFeedItems returns raw feed items
func (m *mockStorage) GetRawFeedItems(limit int) ([]models.RawFeedItem, error) {
	return []models.RawFeedItem{}, nil
}

// CleanupExpiredRawFeedItems cleans up expired raw feed items
func (m *mockStorage) CleanupExpiredRawFeedItems() error {
	return nil
}

// UpdateAIScore updates the AI score for a torrent
func (m *mockStorage) UpdateAIScore(id int, score float64, reason string) error {
	return nil
}

// setupTestServer creates a test server instance
func setupTestServer(t *testing.T) (*Server, *mockStorage) {
	logger, _ := zap.NewProduction()
	store := &mockStorage{
		torrents:   make(map[int]*models.StagedTorrent),
		activities: []models.Activity{},
	}

	return &Server{
		store:  store,
		client: nil, // No qBittorrent client for testing
		logger: logger,
		port:   8081,
	}, store
}

// createTestTorrent creates a test torrent
func createTestTorrent(id int, status string) *models.StagedTorrent {
	return &models.StagedTorrent{
		ID:          id,
		Status:      status,
		MatchReason: "test match",
		StagedAt:    time.Now(),
		FeedItem: models.FeedItem{
			Title: "Test Torrent",
			Link:  "http://example.com/torrent.torrent",
			GUID:  "http://example.com/torrent-1",
			Size:  1024 * 1024,
		},
	}
}

// TestHandleApprove tests the approve handler with a pending torrent
func TestHandleApprove(t *testing.T) {
	server, mockStore := setupTestServer(t)

	// Setup test torrent
	torrent := createTestTorrent(1, "pending")
	mockStore.torrents[1] = torrent

	// Create request
	req := httptest.NewRequest("POST", "/api/torrents/1/approve", nil)
	w := httptest.NewRecorder()

	// Call handler
	server.handleApprove(w, req, 1)

	// Verify response
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp ApproveResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "accepted" {
		t.Errorf("expected status 'accepted', got '%s'", resp.Status)
	}

	// Verify torrent was updated
	if mockStore.torrents[1].Status != "accepted" {
		t.Errorf("expected torrent status 'accepted', got '%s'", mockStore.torrents[1].Status)
	}
}

// TestHandleApproveNonPending tests approve on a non-pending torrent
func TestHandleApproveNonPending(t *testing.T) {
	server, mockStore := setupTestServer(t)

	// Setup already-accepted torrent
	torrent := createTestTorrent(1, "accepted")
	mockStore.torrents[1] = torrent

	req := httptest.NewRequest("POST", "/api/torrents/1/approve", nil)
	w := httptest.NewRecorder()

	server.handleApprove(w, req, 1)

	// Should return 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

// TestHandleApproveNotFound tests approve on a non-existent torrent
func TestHandleApproveNotFound(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/torrents/999/approve", nil)
	w := httptest.NewRecorder()

	server.handleApprove(w, req, 999)

	// Should return 404 Not Found
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

// TestHandleReject tests the reject handler
func TestHandleReject(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "pending")
	mockStore.torrents[1] = torrent

	req := httptest.NewRequest("POST", "/api/torrents/1/reject", nil)
	w := httptest.NewRecorder()

	server.handleReject(w, req, 1)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Verify torrent was rejected
	if mockStore.torrents[1].Status != "rejected" {
		t.Errorf("expected torrent status 'rejected', got '%s'", mockStore.torrents[1].Status)
	}
}

// TestHandleRejectNonPending tests reject on a non-pending torrent
func TestHandleRejectNonPending(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "accepted")
	mockStore.torrents[1] = torrent

	req := httptest.NewRequest("POST", "/api/torrents/1/reject", nil)
	w := httptest.NewRecorder()

	server.handleReject(w, req, 1)

	// Should return 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

// TestHandleRejectNotFound tests reject on a non-existent torrent
func TestHandleRejectNotFound(t *testing.T) {
	server, _ := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/torrents/999/reject", nil)
	w := httptest.NewRecorder()

	server.handleReject(w, req, 999)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

// TestHandleQueueWithoutClient tests queue without qBittorrent client
func TestHandleQueueWithoutClient(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "accepted")
	mockStore.torrents[1] = torrent

	body := bytes.NewBufferString(`{"tags": "test", "category": ""}`)
	req := httptest.NewRequest("POST", "/api/torrents/1/queue", body)
	w := httptest.NewRecorder()

	server.handleQueue(w, req, 1)

	// Should return 503 Service Unavailable (no qBittorrent client)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", w.Code)
	}
}

// TestHandleQueueNonAccepted tests queue on non-accepted torrent
func TestHandleQueueNonAccepted(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "pending")
	mockStore.torrents[1] = torrent

	body := bytes.NewBufferString(`{"tags": "test", "category": ""}`)
	req := httptest.NewRequest("POST", "/api/torrents/1/queue", body)
	w := httptest.NewRecorder()

	server.handleQueue(w, req, 1)

	// Should return 400 Bad Request
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

// TestHandleQueueNotFound tests queue on non-existent torrent
func TestHandleQueueNotFound(t *testing.T) {
	server, _ := setupTestServer(t)

	body := bytes.NewBufferString(`{"tags": "test", "category": ""}`)
	req := httptest.NewRequest("POST", "/api/torrents/999/queue", body)
	w := httptest.NewRecorder()

	server.handleQueue(w, req, 999)

	// Should return 404 Not Found
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

// TestActivityLogging tests that activities are logged correctly
func TestActivityLogging(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "pending")
	mockStore.torrents[1] = torrent

	req := httptest.NewRequest("POST", "/api/torrents/1/reject", nil)
	w := httptest.NewRecorder()

	server.handleReject(w, req, 1)

	// Verify activity was logged
	if len(mockStore.activities) != 1 {
		t.Fatalf("expected 1 activity logged, got %d", len(mockStore.activities))
	}

	activity := mockStore.activities[0]
	if activity.Action != "reject" {
		t.Errorf("expected action 'reject', got '%s'", activity.Action)
	}
	if activity.TorrentID != 1 {
		t.Errorf("expected torrent ID 1, got %d", activity.TorrentID)
	}
}

// TestMultipleStatusTransitions tests valid status transition sequences
func TestMultipleStatusTransitions(t *testing.T) {
	server, mockStore := setupTestServer(t)

	torrent := createTestTorrent(1, "pending")
	mockStore.torrents[1] = torrent

	// First, approve (pending -> accepted)
	req1 := httptest.NewRequest("POST", "/api/torrents/1/approve", nil)
	w1 := httptest.NewRecorder()
	server.handleApprove(w1, req1, 1)

	if w1.Code != http.StatusOK {
		t.Fatalf("approve failed with status %d", w1.Code)
	}

	// Verify status changed
	if mockStore.torrents[1].Status != "accepted" {
		t.Fatalf("expected status 'accepted' after approve, got '%s'", mockStore.torrents[1].Status)
	}

	// Now test that we cannot approve again
	req2 := httptest.NewRequest("POST", "/api/torrents/1/approve", nil)
	w2 := httptest.NewRecorder()
	server.handleApprove(w2, req2, 1)

	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when approving already-accepted torrent, got %d", w2.Code)
	}

	// Test that we cannot reject an accepted torrent
	req3 := httptest.NewRequest("POST", "/api/torrents/1/reject", nil)
	w3 := httptest.NewRecorder()
	server.handleReject(w3, req3, 1)

	if w3.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when rejecting accepted torrent, got %d", w3.Code)
	}
}
