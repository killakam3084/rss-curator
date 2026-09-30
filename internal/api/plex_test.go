package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/killakam3084/rss-curator/internal/settings"
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

func TestPlexMetaTestsConnectionWhenDisabled(t *testing.T) {
	plexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "test-token" {
			t.Errorf("X-Plex-Token = %q, want test-token", r.Header.Get("X-Plex-Token"))
		}
		switch r.URL.Path {
		case "/identity":
			w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"test","version":"1.40"}}`))
		case "/library/sections":
			w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`))
		default:
			t.Errorf("unexpected Plex request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer plexServer.Close()

	server, mockStore := setupTestServer(t)
	manager := settings.NewManager(mockStore)
	if err := manager.Load(settings.EnvDefaults{}); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	server.settingsMgr = manager

	// Missing credentials are reported as configuration errors before any
	// connection is attempted.
	missingConfig := httptest.NewRecorder()
	server.handlePlexMeta(missingConfig, httptest.NewRequest(http.MethodGet, "/api/plex/meta", nil))
	var missingResp plexMetaResponse
	if err := json.Unmarshal(missingConfig.Body.Bytes(), &missingResp); err != nil {
		t.Fatalf("decode missing-config response: %v", err)
	}
	if missingResp.Connected || missingResp.Error != "plex URL and token are required to test the connection" {
		t.Fatalf("missing-config response = %+v", missingResp)
	}

	configured := manager.Get()
	configured.Plex.URL = "not a URL"
	configured.Plex.Token = "test-token"
	if err := manager.Update(configured); err != nil {
		t.Fatalf("save invalid Plex settings: %v", err)
	}
	invalidConfig := httptest.NewRecorder()
	server.handlePlexMeta(invalidConfig, httptest.NewRequest(http.MethodGet, "/api/plex/meta", nil))
	var invalidResp plexMetaResponse
	if err := json.Unmarshal(invalidConfig.Body.Bytes(), &invalidResp); err != nil {
		t.Fatalf("decode invalid-config response: %v", err)
	}
	if invalidResp.Connected || !strings.HasPrefix(invalidResp.Error, "invalid plex configuration:") {
		t.Fatalf("invalid-config response = %+v", invalidResp)
	}

	configured = manager.Get()
	configured.Plex.URL = plexServer.URL
	configured.Plex.Token = "test-token"
	if err := manager.Update(configured); err != nil {
		t.Fatalf("save Plex settings: %v", err)
	}
	if configured.Plex.Enabled {
		t.Fatal("test requires Plex integration to remain disabled")
	}

	rec := httptest.NewRecorder()
	server.handlePlexMeta(rec, httptest.NewRequest(http.MethodGet, "/api/plex/meta", nil))
	var resp plexMetaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode connection response: %v", err)
	}
	if !resp.Connected || resp.ServerVersion != "1.40" || len(resp.Libraries) != 1 {
		t.Fatalf("connection response = %+v", resp)
	}

	configured = manager.Get()
	configured.Plex.URL = "http://127.0.0.1:1"
	if err := manager.Update(configured); err != nil {
		t.Fatalf("save unreachable Plex settings: %v", err)
	}
	unreachable := httptest.NewRecorder()
	server.handlePlexMeta(unreachable, httptest.NewRequest(http.MethodGet, "/api/plex/meta", nil))
	var unreachableResp plexMetaResponse
	if err := json.Unmarshal(unreachable.Body.Bytes(), &unreachableResp); err != nil {
		t.Fatalf("decode unreachable response: %v", err)
	}
	if unreachableResp.Connected || !strings.HasPrefix(unreachableResp.Error, "plex connection failed:") {
		t.Fatalf("unreachable response = %+v", unreachableResp)
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
	if resp.Libraries == nil || resp.Annotations == nil || resp.AnnotationsByStatus == nil {
		t.Error("collections must serialise as empty, not null")
	}
	for _, status := range []string{"pending", "accepted"} {
		if resp.AnnotationsByStatus[status] == nil {
			t.Errorf("annotations_by_status.%s must be initialized", status)
		}
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
