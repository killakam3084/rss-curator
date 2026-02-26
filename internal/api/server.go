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
	fmt.Printf("[API] Server listening on 0.0.0.0%s\n", addr)

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

	fmt.Printf("[API] handleList: status=%s\n", status)

	torrents, err := s.store.List(status)
	if err != nil {
		fmt.Printf("[API] Error listing torrents: %v\n", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	fmt.Printf("[API] Found %d torrents with status %s\n", len(torrents), status)

	resp := ListResponse{
		Torrents: make([]TorrentResponse, 0),
		Count:    len(torrents),
	}

	for _, t := range torrents {
		fmt.Printf("[API] Torrent: id=%d, title=%s, status=%s\n", t.ID, t.FeedItem.Title, t.Status)
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
	fmt.Printf("[API] handleTorrentAction: path=%s, method=%s\n", path, r.Method)

	parts := strings.Split(path, "/")
	fmt.Printf("[API] path parts: %v\n", parts)

	if len(parts) < 2 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Invalid request path"})
		return
	}

	idStr := parts[0]
	action := parts[1]
	fmt.Printf("[API] idStr=%s, action=%s\n", idStr, action)

	id, err := strconv.Atoi(idStr)
	if err != nil {
		fmt.Printf("[API] Failed to parse ID: %s, error: %v\n", idStr, err)
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
		fmt.Printf("[API] Unknown action: %s\n", action)
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

	fmt.Printf("[API] handleApprove: id=%d\n", id)

	// Check if qBittorrent is available
	if s.client == nil {
		fmt.Println("[API] qBittorrent client unavailable")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "qBittorrent service unavailable"})
		return
	}

	torrent, err := s.store.Get(id)
	if err != nil {
		fmt.Printf("[API] Error retrieving torrent %d: %v\n", id, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Error retrieving torrent: %v", err)})
		return
	}

	if torrent == nil {
		fmt.Printf("[API] Torrent %d not found in database\n", id)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		fmt.Printf("[API] Torrent %d is already %s, cannot approve\n", id, torrent.Status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	// Add to qBittorrent
	fmt.Printf("[API] Adding torrent %d to qBittorrent: %s\n", id, torrent.FeedItem.Title)
	if err := s.client.AddTorrent(torrent.FeedItem.Link, nil); err != nil {
		fmt.Printf("[API] Error adding torrent %d to qBittorrent: %v\n", id, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to add torrent: %v", err)})
		return
	}

	// Update status in storage
	fmt.Printf("[API] Updating torrent %d status to approved\n", id)
	if err := s.store.UpdateStatus(id, "approved"); err != nil {
		fmt.Printf("[API] Error updating torrent %d status: %v\n", id, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	fmt.Printf("[API] Torrent %d approved successfully\n", id)
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
		fmt.Printf("[API] Error retrieving torrent %d: %v\n", id, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Error retrieving torrent: %v", err)})
		return
	}

	if torrent == nil {
		fmt.Printf("[API] Torrent %d not found in database\n", id)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Torrent not found"})
		return
	}

	if torrent.Status != "pending" {
		fmt.Printf("[API] Torrent %d is already %s, cannot reject\n", id, torrent.Status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	if err := s.store.UpdateStatus(id, "rejected"); err != nil {
		fmt.Printf("[API] Error updating torrent %d status: %v\n", id, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	fmt.Printf("[API] Torrent %d rejected successfully\n", id)
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
