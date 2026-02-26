package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iillmaticc/rss-curator/internal/client"
	"github.com/iillmaticc/rss-curator/internal/storage"
	"go.uber.org/zap"
)

type Server struct {
	store  *storage.Storage
	client *client.Client // May be nil if qBittorrent is unavailable
	logger *zap.Logger
	port   int
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TorrentResponse struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	MatchReason string `json:"match_reason"`
	Status      string `json:"status"`
	Link        string `json:"link"`
}

type ListResponse struct {
	Torrents []TorrentResponse `json:"torrents"`
	Count    int               `json:"count"`
}

type ApproveResponse struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

type RejectResponse struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

// NewServer creates a new API server instance
func NewServer(store *storage.Storage, client *client.Client, port int) *Server {
	// Create a production logger (use development logger in dev if preferred)
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("failed to create logger: %v", err))
	}
	defer logger.Sync() // Flush logs on shutdown

	return &Server{
		store:  store,
		client: client,
		logger: logger,
		port:   port,
	}
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/torrents", s.handleList)
	mux.HandleFunc("/api/torrents/", s.handleTorrentAction)
	mux.HandleFunc("/api/health", s.handleHealth)

	// Static files and UI
	mux.Handle("/style.css", http.FileServer(http.Dir("./web")))
	mux.Handle("/app.js", http.FileServer(http.Dir("./web")))

	// Root and fallback
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf(":%d", s.port)
	s.logger.Info("API server starting", zap.String("address", addr))

	return http.ListenAndServe(addr, mux)
}

// handleRoot serves the dashboard or API info
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Serve the dashboard HTML
	http.ServeFile(w, r, "./web/index.html")
}

// handleList lists torrents by status
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}

	torrents, err := s.store.List(status)
	if err != nil {
		s.logger.Error("failed to list torrents", zap.String("status", status), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("torrents listed", zap.String("status", status), zap.Int("count", len(torrents)))

	resp := ListResponse{
		Torrents: make([]TorrentResponse, 0),
		Count:    len(torrents),
	}

	for _, t := range torrents {
		resp.Torrents = append(resp.Torrents, TorrentResponse{
			ID:          t.ID,
			Title:       t.FeedItem.Title,
			Size:        t.FeedItem.Size,
			MatchReason: t.MatchReason,
			Status:      t.Status,
			Link:        t.FeedItem.Link,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleTorrentAction routes approve/reject requests
func (s *Server) handleTorrentAction(w http.ResponseWriter, r *http.Request) {
	// Remove /api/torrents/ prefix to get {id}/action
	path := strings.TrimPrefix(r.URL.Path, "/api/torrents/")

	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request path"})
		return
	}

	idStr := parts[0]
	action := parts[1]

	id, err := strconv.Atoi(idStr)
	if err != nil {
		s.logger.Warn("invalid torrent ID in request", zap.String("id", idStr), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid torrent ID"})
		return
	}

	switch action {
	case "approve":
		s.handleApprove(w, r, id)
	case "reject":
		s.handleReject(w, r, id)
	default:
		s.logger.Warn("unknown torrent action", zap.Int("id", id), zap.String("action", action))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Unknown action"})
	}
}

// handleApprove approves a torrent and adds it to qBittorrent
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if qBittorrent is available
	if s.client == nil {
		s.logger.Error("qBittorrent client unavailable", zap.Int("torrent_id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "qBittorrent service unavailable"})
		return
	}

	torrent, err := s.store.Get(id)
	if err != nil {
		s.logger.Error("failed to retrieve torrent", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Error retrieving torrent: %v", err)})
		return
	}

	if torrent == nil {
		s.logger.Warn("torrent not found", zap.Int("id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		s.logger.Warn("cannot approve non-pending torrent", zap.Int("id", id), zap.String("status", torrent.Status))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	// Add to qBittorrent
	if err := s.client.AddTorrent(torrent.FeedItem.Link, nil); err != nil {
		s.logger.Error("failed to add torrent to qBittorrent", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to add torrent: %v", err)})
		return
	}

	// Update status in storage
	if err := s.store.UpdateStatus(id, "approved"); err != nil {
		s.logger.Error("failed to update torrent status", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("torrent approved", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApproveResponse{
		ID:     id,
		Status: "approved",
		Title:  torrent.FeedItem.Title,
	})
}

// handleReject rejects a torrent
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	torrent, err := s.store.Get(id)
	if err != nil {
		s.logger.Error("failed to retrieve torrent", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Error retrieving torrent: %v", err)})
		return
	}

	if torrent == nil {
		s.logger.Warn("torrent not found", zap.Int("id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		s.logger.Warn("cannot reject non-pending torrent", zap.Int("id", id), zap.String("status", torrent.Status))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	if err := s.store.UpdateStatus(id, "rejected"); err != nil {
		s.logger.Error("failed to update torrent status", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("torrent rejected", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RejectResponse{
		ID:     id,
		Status: "rejected",
	})
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
}
