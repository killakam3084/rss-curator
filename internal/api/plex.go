package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/metadata"
	"github.com/killakam3084/rss-curator/internal/ops"
	"github.com/killakam3084/rss-curator/internal/storage"
	"go.uber.org/zap"
)

// PlexDeps carries the Plex wiring supplied by the entrypoint. All fields may
// be zero: every handler degrades to a clear "unavailable" response.
type PlexDeps struct {
	Client   *client.Plex
	Store    plexAPIStore
	Metadata *metadata.Lookup
}

// plexAPIStore is the storage surface the Plex endpoints need, combining what
// sync and reconcile each require.
type plexAPIStore interface {
	ops.PlexStore
	ops.PlexReconcileStore
	ListPlexLibraries() ([]storage.PlexLibrary, error)
	CountAnnotationsByKind() (map[string]int, error)
	CountAnnotationsByStatusAndKind() (map[string]map[string]int, error)
}

// WithPlex attaches the Plex integration. Passing a zero PlexDeps leaves the
// endpoints registered but reporting the integration as unavailable.
func (s *Server) WithPlex(deps PlexDeps) *Server {
	s.plex = deps
	return s
}

type plexLibraryResponse struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	ItemCount int    `json:"item_count"`
}

type plexMetaResponse struct {
	Connected     bool                  `json:"connected"`
	ServerVersion string                `json:"server_version,omitempty"`
	Libraries     []plexLibraryResponse `json:"libraries"`
	Error         string                `json:"error,omitempty"`
}

// handlePlexMeta tests the Plex connection and lists library sections.
// GET /api/plex/meta
func (s *Server) handlePlexMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	resp := plexMetaResponse{Libraries: []plexLibraryResponse{}}
	plexClient := s.plex.Client
	if plexClient == nil {
		if s.settingsMgr == nil {
			resp.Error = "plex is not configured"
			json.NewEncoder(w).Encode(resp)
			return
		}
		plexSettings := s.settingsMgr.Get().Plex
		if plexSettings.URL == "" || plexSettings.Token == "" || plexSettings.Token == "***" {
			resp.Error = "plex URL and token are required to test the connection"
			json.NewEncoder(w).Encode(resp)
			return
		}
		var err error
		plexClient, err = client.NewPlex(client.PlexConfig{
			BaseURL: plexSettings.URL,
			Token:   plexSettings.Token,
		})
		if err != nil {
			resp.Error = "invalid plex configuration: " + err.Error()
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	identity, err := plexClient.Identity(r.Context())
	if err != nil {
		resp.Error = "plex connection failed: " + err.Error()
		json.NewEncoder(w).Encode(resp)
		return
	}
	resp.Connected = true
	resp.ServerVersion = identity.Version

	sections, err := plexClient.Sections(r.Context())
	if err != nil {
		resp.Error = "connected, but failed to list plex libraries: " + err.Error()
		json.NewEncoder(w).Encode(resp)
		return
	}
	for _, sec := range sections {
		if sec.Type != "show" && sec.Type != "movie" {
			continue
		}
		resp.Libraries = append(resp.Libraries, plexLibraryResponse{
			Key: sec.Key, Title: sec.Title, Type: sec.Type,
		})
	}
	json.NewEncoder(w).Encode(resp)
}

type plexStatusResponse struct {
	Enabled             bool                      `json:"enabled"`
	SyncEnabled         bool                      `json:"sync_enabled"`
	Libraries           []storage.PlexLibrary     `json:"libraries"`
	Counts              storage.PlexCounts        `json:"counts"`
	Annotations         map[string]int            `json:"annotations"`
	AnnotationsByStatus map[string]map[string]int `json:"annotations_by_status"`
	Error               string                    `json:"error,omitempty"`
	Settings            map[string]interface{}    `json:"settings,omitempty"`
}

// handlePlexStatus reports what is currently cached.
// GET /api/plex/status
func (s *Server) handlePlexStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	resp := plexStatusResponse{
		Libraries: []storage.PlexLibrary{},
		Annotations: map[string]int{
			storage.AnnotationInLibrary:      0,
			storage.AnnotationLibraryUpgrade: 0,
		},
		AnnotationsByStatus: map[string]map[string]int{
			"pending": {
				storage.AnnotationInLibrary:      0,
				storage.AnnotationLibraryUpgrade: 0,
			},
			"accepted": {
				storage.AnnotationInLibrary:      0,
				storage.AnnotationLibraryUpgrade: 0,
			},
		},
	}
	if s.settingsMgr != nil {
		st := s.settingsMgr.Get().Plex
		resp.Enabled = st.Enabled
		resp.SyncEnabled = st.SyncEnabled
	}
	if s.plex.Store == nil {
		resp.Error = "plex is not configured"
		json.NewEncoder(w).Encode(resp)
		return
	}

	if libs, err := s.plex.Store.ListPlexLibraries(); err == nil {
		resp.Libraries = libs
	} else {
		resp.Error = err.Error()
	}
	if counts, err := s.plex.Store.GetPlexCounts(); err == nil {
		resp.Counts = counts
	}
	if ann, err := s.plex.Store.CountAnnotationsByKind(); err == nil {
		for kind, count := range ann {
			resp.Annotations[kind] = count
		}
	}
	if ann, err := s.plex.Store.CountAnnotationsByStatusAndKind(); err == nil {
		for status, counts := range ann {
			if _, ok := resp.AnnotationsByStatus[status]; !ok {
				continue
			}
			for kind, count := range counts {
				resp.AnnotationsByStatus[status][kind] = count
			}
		}
	}
	json.NewEncoder(w).Encode(resp)
}

// handlePlexSync triggers an on-demand library sync.
// POST /api/plex/sync — 202, or 409 when already running.
func (s *Server) handlePlexSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if !s.plexReady(w) {
		return
	}

	var sections []string
	if s.settingsMgr != nil {
		sections = s.settingsMgr.Get().Plex.SectionKeys
	}
	syncCfg := ops.PlexSyncConfig{SectionKeys: sections}
	syncDeps := ops.PlexSyncDeps{
		Store:  s.plex.Store,
		Plex:   s.plex.Client,
		Logger: s.logger,
	}
	reconcileDeps := s.plexReconcileDeps()

	err := s.queue.Submit("plex_sync", false, func(ctx context.Context) {
		if _, err := ops.RunPlexSync(ctx, syncCfg, syncDeps); err != nil {
			s.logger.Error("plex sync failed", zap.Error(err))
			return
		}
		if _, err := ops.RunPlexReconcile(ctx, reconcileDeps); err != nil {
			s.logger.Error("plex reconcile after sync failed", zap.Error(err))
		}
	})
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("plex sync triggered via API")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

// handlePlexReconcile re-annotates staged torrents without re-syncing.
// POST /api/plex/reconcile — 202, or 409 when already running.
func (s *Server) handlePlexReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if s.plex.Store == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "plex is not configured"})
		return
	}
	if s.queue == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "job queue unavailable"})
		return
	}

	deps := s.plexReconcileDeps()
	err := s.queue.Submit("plex_reconcile", false, func(ctx context.Context) {
		if _, err := ops.RunPlexReconcile(ctx, deps); err != nil {
			s.logger.Error("plex reconcile failed", zap.Error(err))
		}
	})
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("plex reconcile triggered via API")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

// plexReady reports whether a sync can run, writing the reason when it cannot.
func (s *Server) plexReady(w http.ResponseWriter) bool {
	switch {
	case s.plex.Client == nil || s.plex.Store == nil:
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "plex is not configured"})
		return false
	case s.queue == nil:
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "job queue unavailable"})
		return false
	}
	return true
}

func (s *Server) plexReconcileDeps() ops.PlexReconcileDeps {
	deps := ops.PlexReconcileDeps{
		Store:  s.plex.Store,
		Plex:   s.plex.Client,
		Logger: s.logger,
	}
	// A nil *metadata.Lookup in an interface field is not a nil interface, so
	// the guid tier is disabled explicitly rather than by assignment.
	if s.plex.Metadata != nil {
		deps.Metadata = s.plex.Metadata
	}
	return deps
}
