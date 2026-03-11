package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/storage"
	"github.com/killakam3084/rss-curator/pkg/models"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AuthConfig holds optional in-app authentication settings.
// When Password is empty, authentication is disabled and the app is accessible
// without credentials (preserving local-dev ergonomics).
type AuthConfig struct {
	Username      string
	Password      string
	SessionSecret []byte
	SessionTTL    time.Duration
}

type Server struct {
	store         storage.Store
	client        *client.Client // May be nil if qBittorrent is unavailable
	logger        *zap.Logger
	logBuffer     *logbuffer.Buffer
	scorer        *ai.Scorer  // May be nil if AI is disabled
	aiProvider    ai.Provider // May be nil if AI is disabled
	port          int
	authEnabled   bool
	authUsername  string
	authPassword  string
	sessionSecret []byte
	sessionTTL    time.Duration
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TorrentResponse struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Size        int64   `json:"size"`
	MatchReason string  `json:"match_reason"`
	Status      string  `json:"status"`
	Link        string  `json:"link"`
	AIScore     float64 `json:"ai_score"`
	AIReason    string  `json:"ai_reason"`
	AIScored    bool    `json:"ai_scored"`
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

type ActivityResponse struct {
	Activities []ActivityItem `json:"activities"`
	Total      int            `json:"total"`
}

type ActivityItem struct {
	ID           int    `json:"id"`
	TorrentID    int    `json:"torrent_id"`
	TorrentTitle string `json:"torrent_title"`
	Action       string `json:"action"`
	ActionAt     string `json:"action_at"`
	MatchReason  string `json:"match_reason"`
}

type StatsResponse struct {
	Hours    int `json:"hours"`
	Pending  int `json:"pending"`
	Seen     int `json:"seen"`
	Staged   int `json:"staged"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
	Queued   int `json:"queued"`
}

type RescoreRequest struct {
	IDs []int `json:"ids"`
}

type RescoreResponse struct {
	Rescored int               `json:"rescored"`
	Torrents []TorrentResponse `json:"torrents"`
}

// SuggestionsResponse is the shape returned by POST /api/suggestions.
// Currently returns 501 Not Implemented — the Suggester subsystem is
// under active development. This stub establishes the API contract.
type SuggestionsResponse struct {
	Suggestions []interface{} `json:"suggestions"`
	Status      string        `json:"status"`
}

type FeedStreamItem struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	Size         int64  `json:"size"`
	MatchReason  string `json:"match_reason"`
	Status       string `json:"status"`
	Link         string `json:"link"`
	DiscoveredAt string `json:"discovered_at"`
}

type FeedStreamResponse struct {
	Items []FeedStreamItem `json:"items"`
	Total int              `json:"total"`
}

// NewServer creates a new API server instance.
// buf may be nil; when provided, logs are tee'd into it for /api/logs streaming.
// scorer and aiProvider may be nil when AI is disabled.
func NewServer(store storage.Store, client *client.Client, port int, buf *logbuffer.Buffer, scorer *ai.Scorer, aiProvider ai.Provider, auth AuthConfig) *Server {
	prodCore, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("failed to create logger: %v", err))
	}

	var logger *zap.Logger
	if buf != nil {
		core := zapcore.NewTee(prodCore.Core(), logbuffer.NewZapCore(buf))
		logger = zap.New(core, zap.AddCaller())
	} else {
		logger = prodCore
	}

	// Wire the tee logger into the scorer so all LLM I/O surfaces in the log drawer.
	if scorer != nil {
		scorer.SetLogger(logger)
	}

	return &Server{
		store:         store,
		client:        client,
		logger:        logger,
		logBuffer:     buf,
		scorer:        scorer,
		aiProvider:    aiProvider,
		port:          port,
		authEnabled:   auth.Password != "",
		authUsername:  auth.Username,
		authPassword:  auth.Password,
		sessionSecret: auth.SessionSecret,
		sessionTTL:    auth.SessionTTL,
	}
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/torrents", s.handleList)
	mux.HandleFunc("/api/torrents/rescore", s.handleRescore)
	mux.HandleFunc("/api/torrents/", s.handleTorrentAction)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/activity", s.handleActivity)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/feed/stream", s.handleFeedStream)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/stream", s.handleLogsStream)
	mux.HandleFunc("/api/suggestions", s.handleSuggestions)

	// Static files and UI
	mux.Handle("/style.css", http.FileServer(http.Dir("./web")))
	mux.Handle("/app.js", http.FileServer(http.Dir("./web")))

	// Auth routes (registered unconditionally; handleLogin/handleLogout are
	// no-ops when auth is disabled because the middleware never blocks access)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Root and fallback
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf(":%d", s.port)

	var handler http.Handler = mux
	if s.authEnabled {
		handler = authMiddleware(mux, s.sessionSecret)
		s.logger.Info("Auth enabled", zap.String("username", s.authUsername))
	}

	s.logger.Info("API server starting", zap.String("address", addr))
	return http.ListenAndServe(addr, handler)
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
			AIScore:     t.AIScore,
			AIReason:    t.AIReason,
			AIScored:    t.AIScored,
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
	case "queue":
		s.handleQueue(w, r, id)
	case "retry-qb":
		s.handleRetryQBittorrent(w, r, id)
	default:
		s.logger.Warn("unknown torrent action", zap.Int("id", id), zap.String("action", action))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "Unknown action"})
	}
}

// handleApprove marks a torrent as approved (tollgate entry)
// Does NOT queue to qBittorrent - that happens in handleQueue
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request, id int) {
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
		s.logger.Warn("cannot approve non-pending torrent", zap.Int("id", id), zap.String("status", torrent.Status))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent already %s", torrent.Status)})
		return
	}

	// Update status to accepted (tollgate passed - awaiting download queue or deferral)
	if err := s.store.UpdateStatus(id, "accepted"); err != nil {
		s.logger.Error("failed to update torrent status", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Log the approve action so it is available as training data for the AI scorer.
	if err := s.store.LogActivity(id, torrent.FeedItem.Title, "approve", torrent.MatchReason); err != nil {
		s.logger.Warn("failed to log approve activity", zap.Int("id", id), zap.Error(err))
	}

	s.logger.Info("torrent accepted and awaiting queue decision", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApproveResponse{
		ID:     id,
		Status: "accepted",
		Title:  torrent.FeedItem.Title,
	})
}

// handleQueue queues an accepted torrent for download to qBittorrent
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse queue configuration from request body
	var queueConfig struct {
		SavePath string `json:"savePath"`
		Tags     string `json:"tags"`
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&queueConfig); err != nil {
		// Configuration is optional, so don't fail if body is empty
		s.logger.Debug("no queue configuration provided", zap.Error(err))
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

	if torrent.Status != "accepted" {
		s.logger.Warn("can only queue accepted torrents", zap.Int("id", id), zap.String("status", torrent.Status))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Torrent is %s, must be accepted to queue", torrent.Status)})
		return
	}

	// Add to qBittorrent (blocking call)
	if s.client == nil {
		s.logger.Error("qBittorrent client unavailable - cannot queue torrent", zap.Int("id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "qBittorrent unavailable"})
		return
	}

	if err := s.client.AddTorrent(torrent.FeedItem.Link, map[string]string{
		"title":    torrent.FeedItem.Title,
		"savePath": queueConfig.SavePath,
		"tags":     queueConfig.Tags,
		"category": queueConfig.Category,
	}); err != nil {
		s.logger.Error("failed to add torrent to qBittorrent", zap.Int("id", id), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to queue: %v", err)})
		return
	}

	// Update status to show it's been queued
	// Note: In production, you might track actual download state from qBittorrent
	// For now, we'll keep it as "approved" since the source of truth is qBittorrent
	if err := s.store.LogActivity(id, torrent.FeedItem.Title, "queue", torrent.MatchReason); err != nil {
		s.logger.Error("failed to log activity", zap.Int("id", id), zap.Error(err))
		// Don't fail the request
	}

	s.logger.Info("torrent queued for download", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ApproveResponse{
		ID:     id,
		Status: "queued",
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

	// Log the activity
	if err := s.store.LogActivity(id, torrent.FeedItem.Title, "reject", torrent.MatchReason); err != nil {
		s.logger.Error("failed to log activity", zap.Int("id", id), zap.Error(err))
		// Don't fail the request, just log the error
	}

	s.logger.Info("torrent rejected", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RejectResponse{
		ID:     id,
		Status: "rejected",
	})
}

// handleRetryQBittorrent manually retries adding an accepted torrent to qBittorrent
func (s *Server) handleRetryQBittorrent(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.client == nil {
		s.logger.Warn("qBittorrent client unavailable for retry", zap.Int("id", id))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "qBittorrent client is not available"})
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

	if torrent.Status != "accepted" {
		s.logger.Warn("can only retry accepted torrents", zap.Int("id", id), zap.String("status", torrent.Status))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Can only retry accepted torrents, this one is %s", torrent.Status)})
		return
	}

	s.logger.Info("manually retrying torrent add to qBittorrent", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title), zap.String("link", torrent.FeedItem.Link))

	// Use blocking retry with context timeout
	ctx, cancel := getContextWithTimeout(r.Context())
	defer cancel()

	opts := map[string]string{
		"title": torrent.FeedItem.Title,
	}
	err = s.client.RetryAddTorrent(ctx, torrent.FeedItem.Link, opts)
	if err != nil {
		s.logger.Error("retry failed to add torrent to qBittorrent", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title), zap.String("link", torrent.FeedItem.Link), zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprintf("Failed to add to qBittorrent: %v", err)})
		return
	}

	s.logger.Info("torrent successfully added to qBittorrent via retry", zap.Int("id", id), zap.String("title", torrent.FeedItem.Title))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     id,
		"status": "retry_successful",
		"title":  torrent.FeedItem.Title,
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

// handleActivity returns activity log
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if lim, err := strconv.Atoi(l); err == nil && lim > 0 && lim <= 100 {
			limit = lim
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if off, err := strconv.Atoi(o); err == nil && off >= 0 {
			offset = off
		}
	}

	action := r.URL.Query().Get("action")
	if action != "" && action != "approve" && action != "reject" {
		action = ""
	}

	activities, err := s.store.GetActivity(limit, offset, action)
	if err != nil {
		s.logger.Error("failed to get activities", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	// Convert to response items
	items := make([]ActivityItem, 0)
	for _, a := range activities {
		items = append(items, ActivityItem{
			ID:           a.ID,
			TorrentID:    a.TorrentID,
			TorrentTitle: a.TorrentTitle,
			Action:       a.Action,
			ActionAt:     a.ActionAt.Format("2006-01-02 15:04:05"),
			MatchReason:  a.MatchReason,
		})
	}

	total, err := s.store.GetActivityCount(action)
	if err != nil {
		s.logger.Error("failed to get activity count", zap.Error(err))
		total = 0
	}

	s.logger.Info("activities retrieved", zap.Int("count", len(activities)), zap.String("action", action))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ActivityResponse{
		Activities: items,
		Total:      total,
	})
}

// handleStats returns 24-hour windowed torrent statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ws, err := s.store.GetWindowStats(24)
	if err != nil {
		s.logger.Error("failed to get window stats", zap.Error(err))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	s.logger.Info("stats retrieved", zap.Int("pending", ws.Pending), zap.Int("seen_24h", ws.Seen))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StatsResponse{
		Hours:    ws.Hours,
		Pending:  ws.Pending,
		Seen:     ws.Seen,
		Staged:   ws.Staged,
		Approved: ws.Approved,
		Rejected: ws.Rejected,
		Queued:   ws.Queued,
	})
}

// getContextWithTimeout wraps a context with a 30 second timeout
func getContextWithTimeout(baseCtx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(baseCtx, 30*time.Second)
}

// handleLogs returns buffered log entries as JSON.
// Accepts an optional ?since=<id> query param for incremental polling.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.logBuffer == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]logbuffer.LogEntry{})
		return
	}
	var sinceID uint64
	if raw := r.URL.Query().Get("since"); raw != "" {
		if v, err := strconv.ParseUint(raw, 10, 64); err == nil {
			sinceID = v
		}
	}
	entries := s.logBuffer.Entries(sinceID)
	if entries == nil {
		entries = []logbuffer.LogEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// handleLogsStream streams live log entries over Server-Sent Events.
func (s *Server) handleLogsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if s.logBuffer == nil {
		// Buffer not wired; send a single comment and close.
		fmt.Fprintf(w, ": log buffer not available\n\n")
		fl.Flush()
		return
	}

	ch, unsub := s.logBuffer.Subscribe()
	defer unsub()

	// Send a keep-alive comment immediately so the browser knows the stream opened.
	fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	for {
		select {
		case entry, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// handleFeedStream returns recent RSS feed discoveries
func (s *Server) handleFeedStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get limit from query params (default 100)
	limitStr := r.URL.Query().Get("limit")
	limit := 100
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	// Get raw feed items (unfiltered torrents pulled from RSS feeds)
	rawItems, err := s.store.GetRawFeedItems(limit)
	if err != nil {
		s.logger.Error("failed to get raw feed items", zap.Error(err))
		http.Error(w, "Failed to fetch feed stream", http.StatusInternalServerError)
		return
	}

	s.logger.Debug("retrieved raw feed items", zap.Int("count", len(rawItems)))

	// Clean up expired items in background
	go func() {
		if err := s.store.CleanupExpiredRawFeedItems(); err != nil {
			s.logger.Error("failed to cleanup expired raw feed items", zap.Error(err))
		}
	}()

	// Convert to response format
	items := make([]FeedStreamItem, len(rawItems))
	for i, r := range rawItems {
		items[i] = FeedStreamItem{
			ID:           r.ID,
			Title:        r.FeedItem.Title,
			Size:         r.FeedItem.Size,
			MatchReason:  "",           // Raw feed items don't have match reasons
			Status:       "discovered", // Mark as discovered (not matched/approved/rejected yet)
			Link:         r.FeedItem.Link,
			DiscoveredAt: r.PulledAt.Format(time.RFC3339),
		}
	}

	s.logger.Info("feed stream retrieved", zap.Int("count", len(items)))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(FeedStreamResponse{
		Items: items,
		Total: len(items),
	})
}

// handleRescore force re-scores a specific set of torrents on demand,
// bypassing the ai_scored gate used by the background backfill.
// POST /api/torrents/rescore  body: {"ids":[1,2,3]}
func (s *Server) handleRescore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if s.aiProvider == nil || !s.aiProvider.Available() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "AI provider unavailable"})
		return
	}

	var req RescoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "request body must include at least one id"})
		return
	}

	history, _ := s.store.GetActivity(50, 0, "")

	var updated []TorrentResponse
	for _, id := range req.IDs {
		t, err := s.store.GetByID(id)
		if err != nil || t == nil {
			continue
		}
		scored := s.scorer.ScoreAll([]models.StagedTorrent{*t}, history)
		if len(scored) == 0 {
			continue
		}
		result := scored[0]
		if err := s.store.UpdateAIScore(t.ID, result.AIScore, result.AIReason); err != nil {
			s.logger.Error("failed to update AI score", zap.Int("id", t.ID), zap.Error(err))
			continue
		}
		updated = append(updated, TorrentResponse{
			ID:          result.ID,
			Title:       result.FeedItem.Title,
			Size:        result.FeedItem.Size,
			MatchReason: result.MatchReason,
			Status:      result.Status,
			Link:        result.FeedItem.Link,
			AIScore:     result.AIScore,
			AIReason:    result.AIReason,
			AIScored:    true,
		})
	}

	s.logger.Info("torrents rescored", zap.Int("count", len(updated)))
	json.NewEncoder(w).Encode(RescoreResponse{
		Rescored: len(updated),
		Torrents: updated,
	})
}

// handleSuggestions is the stub for the Suggester subsystem.
// POST /api/suggestions — returns 501 Not Implemented until the engine is live.
// The response shape is stable: clients can integrate the endpoint now.
func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(SuggestionsResponse{
		Suggestions: []interface{}{},
		Status:      "not_implemented",
	})
}
