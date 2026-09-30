package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The Plex endpoints must stay registered and well-behaved when the
// integration is not configured, since the frontend calls them unconditionally.

func TestPlexMetaUnconfigured(t *testing.T) {
	server, _ := setupTestServer(t)

	rec := httptest.NewRecorder()
	server.handlePlexMeta(rec, httptest.NewRequest(http.MethodGet, "/api/plex/meta", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp plexMetaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Connected {
		t.Error("Connected = true with no client")
	}
	if resp.Error == "" {
		t.Error("expected an error message explaining the integration is off")
	}
	// An absent list must serialise as [] so the UI can iterate it directly.
	if resp.Libraries == nil {
		t.Error("Libraries = null, want an empty array")
	}
}

func TestPlexStatusUnconfigured(t *testing.T) {
	server, _ := setupTestServer(t)

	rec := httptest.NewRecorder()
	server.handlePlexStatus(rec, httptest.NewRequest(http.MethodGet, "/api/plex/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp plexStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Enabled {
		t.Error("Enabled = true by default; the integration is opt-in")
	}
	if resp.Libraries == nil || resp.Annotations == nil {
		t.Error("collections must serialise as empty, not null")
	}
}

func TestPlexSyncUnconfigured(t *testing.T) {
	server, _ := setupTestServer(t)

	rec := httptest.NewRecorder()
	server.handlePlexSync(rec, httptest.NewRequest(http.MethodPost, "/api/plex/sync", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestPlexReconcileUnconfigured(t *testing.T) {
	server, _ := setupTestServer(t)

	rec := httptest.NewRecorder()
	server.handlePlexReconcile(rec, httptest.NewRequest(http.MethodPost, "/api/plex/reconcile", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListAnnotationFilterFailsClosed(t *testing.T) {
	server, mockStore := setupTestServer(t)
	mockStore.torrents[1] = createTestTorrent(1, "pending")

	// Without the filter the torrent is listed, and annotations serialise as
	// an empty array rather than null.
	rec := httptest.NewRecorder()
	server.handleList(rec, httptest.NewRequest(http.MethodGet, "/api/torrents?status=pending", nil))
	var all ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if all.Count != 1 {
		t.Fatalf("Count = %d, want 1", all.Count)
	}
	if all.Torrents[0].Annotations == nil {
		t.Error("Annotations = null, want an empty array")
	}

	// Asking to filter when annotations cannot be read must return nothing
	// rather than everything — showing unannotated torrents under an
	// "in library" filter would invite a wrong bulk rejection.
	rec = httptest.NewRecorder()
	server.handleList(rec,
		httptest.NewRequest(http.MethodGet, "/api/torrents?status=pending&annotation=in_library", nil))
	var filtered ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if filtered.Count != 0 {
		t.Errorf("Count = %d, want 0", filtered.Count)
	}
	if filtered.Torrents == nil {
		t.Error("Torrents = null, want an empty array")
	}
}

func TestPlexEndpointsRejectWrongMethods(t *testing.T) {
	server, _ := setupTestServer(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"meta rejects POST", server.handlePlexMeta, http.MethodPost, "/api/plex/meta"},
		{"status rejects POST", server.handlePlexStatus, http.MethodPost, "/api/plex/status"},
		{"sync rejects GET", server.handlePlexSync, http.MethodGet, "/api/plex/sync"},
		{"reconcile rejects GET", server.handlePlexReconcile, http.MethodGet, "/api/plex/reconcile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
		})
	}
}
