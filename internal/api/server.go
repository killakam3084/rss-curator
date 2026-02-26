package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/iillmaticc/rss-curator/internal/client"
	"github.com/iillmaticc/rss-curator/internal/storage"
)

type Server struct {
	store  *storage.Storage
	client *client.Client // May be nil if qBittorrent is unavailable
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
	return &Server{
		store:  store,
		client: client,
		port:   port,
	}
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/torrents", s.handleList)
	mux.HandleFunc("/torrents/", s.handleTorrentAction)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf(":%d", s.port)
	fmt.Printf("[API] Server listening on 0.0.0.0%s\n", addr)

	return http.ListenAndServe(addr, mux)
}

// handleRoot returns basic API info
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"service":   "rss-curator",
		"version":   "0.3.0",
		"endpoints": "/torrents, /torrents/{id}/approve, /torrents/{id}/reject, /health",
	})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

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
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/torrents/"), "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid request path", http.StatusBadRequest)
		return
	}

	idStr := parts[0]
	action := parts[1]

	id, err := strconv.Atoi(idStr)
	if err != nil {
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
		http.Error(w, "Unknown action", http.StatusBadRequest)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "qBittorrent service unavailable"})
		return
	}

	torrent, err := s.store.Get(id)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	// Add to qBittorrent
	if err := s.client.AddTorrent(torrent.FeedItem.Link, nil); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to add torrent: %v", err)})
		return
	}

	// Update status in storage
	if err := s.store.UpdateStatus(id, "approved"); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	if err := s.store.UpdateStatus(id, "rejected"); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

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
