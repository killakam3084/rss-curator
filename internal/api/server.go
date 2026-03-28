package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/killakam3084/rss-curator/internal/ai"
	"github.com/killakam3084/rss-curator/internal/client"
	"github.com/killakam3084/rss-curator/internal/jobs"
	"github.com/killakam3084/rss-curator/internal/logbuffer"
	"github.com/killakam3084/rss-curator/internal/matcher"
	"github.com/killakam3084/rss-curator/internal/ops"
	"github.com/killakam3084/rss-curator/internal/scheduler"
	"github.com/killakam3084/rss-curator/internal/settings"
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
	store            storage.Store
	client           *client.Client // May be nil if qBittorrent is unavailable
	logger           *zap.Logger
	logBuffer        *logbuffer.Buffer
	scorer           *ai.Scorer  // May be nil if AI is disabled
	aiProvider       ai.Provider // May be nil if AI is disabled
	matcher          *matcher.Matcher
	enricher         *ai.Enricher
	port             int
	authEnabled      bool
	authUsername     string
	authPassword     string
	sessionSecret    []byte
	sessionTTL       time.Duration
	scheduler        *scheduler.Scheduler // May be nil if scheduler is not configured
	queue            *jobs.Queue          // May be nil if queue is not configured
	jobCancelMu      sync.Mutex
	jobCancels       map[int]*jobCancelState
	progressInterval int
	settingsMgr      *settings.Manager // may be nil
}

type jobCancelState struct {
	jobType         string
	cancelRequested bool
	runCancel       context.CancelFunc
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TorrentResponse struct {
	ID                    int     `json:"id"`
	Title                 string  `json:"title"`
	Size                  int64   `json:"size"`
	MatchReason           string  `json:"match_reason"`
	Status                string  `json:"status"`
	Link                  string  `json:"link"`
	AIScore               float64 `json:"ai_score"`
	AIReason              string  `json:"ai_reason"`
	AIScored              bool    `json:"ai_scored"`
	MatchConfidence       float64 `json:"match_confidence"`
	MatchConfidenceReason string  `json:"match_confidence_reason"`
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

type RematchRequest struct {
	IDs           []int `json:"ids"`
	AutoRescore   bool  `json:"auto_rescore"`
	ForceAIEnrich bool  `json:"force_ai_enrich"`
}

type RematchResponse struct {
	Rematched       int               `json:"rematched"`
	NoLongerMatches int               `json:"no_longer_matches"`
	Rescored        int               `json:"rescored"`
	Skipped         int               `json:"skipped"`
	Torrents        []TorrentResponse `json:"torrents"`
}

func torrentToResponse(t models.StagedTorrent) TorrentResponse {
	return TorrentResponse{
		ID:                    t.ID,
		Title:                 t.FeedItem.Title,
		Size:                  t.FeedItem.Size,
		MatchReason:           t.MatchReason,
		Status:                t.Status,
		Link:                  t.FeedItem.Link,
		AIScore:               t.AIScore,
		AIReason:              t.AIReason,
		AIScored:              t.AIScored,
		MatchConfidence:       t.MatchConfidence,
		MatchConfidenceReason: t.MatchConfidenceReason,
	}
}

// SuggestionsResponse is the shape returned by POST /api/suggestions.
// Currently returns 501 Not Implemented — the Suggester subsystem is
// under active development. This stub establishes the API contract.
type SuggestionsResponse struct {
	Suggestions []interface{} `json:"suggestions"`
	Status      string        `json:"status"`
}

type SchedulerRunResponse struct {
	Status string `json:"status"`
	Type   string `json:"type"`
}

type JobAcceptedResponse struct {
	JobID  int    `json:"job_id"`
	Status string `json:"status"`
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
func NewServer(store storage.Store, client *client.Client, port int, buf *logbuffer.Buffer, scorer *ai.Scorer, aiProvider ai.Provider, m *matcher.Matcher, enricher *ai.Enricher, auth AuthConfig) *Server {
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
		store:            store,
		client:           client,
		logger:           logger,
		logBuffer:        buf,
		scorer:           scorer,
		aiProvider:       aiProvider,
		matcher:          m,
		enricher:         enricher,
		port:             port,
		authEnabled:      auth.Password != "",
		authUsername:     auth.Username,
		authPassword:     auth.Password,
		sessionSecret:    auth.SessionSecret,
		sessionTTL:       auth.SessionTTL,
		jobCancels:       make(map[int]*jobCancelState),
		progressInterval: 5, // default: emit every 5 items
	}
}

// WithScheduler attaches a Scheduler to the server, enabling the
// /api/scheduler/* endpoints. Returns the server for call chaining.
func (s *Server) WithScheduler(sched *scheduler.Scheduler) *Server {
	s.scheduler = sched
	return s
}

// WithQueue attaches a job Queue to the server. Returns the server for
// call chaining.
func (s *Server) WithQueue(q *jobs.Queue) *Server {
	s.queue = q
	return s
}

func (s *Server) WithProgressInterval(n int) *Server {
	if n > 0 {
		s.progressInterval = n
	}
	return s
}

// WithSettings attaches a settings Manager. The server will apply the current
// settings immediately via applySettings and honour live updates on PATCH.
func (s *Server) WithSettings(mgr *settings.Manager) *Server {
	s.settingsMgr = mgr
	s.applySettings(mgr.Get())
	return s
}

// Start begins listening for HTTP requests
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/torrents", s.handleList)
	mux.HandleFunc("/api/torrents/rescore", s.handleRescore)
	mux.HandleFunc("/api/torrents/rematch", s.handleRematch)
	mux.HandleFunc("/api/torrents/", s.handleTorrentAction)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/activity", s.handleActivity)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/feed/stream", s.handleFeedStream)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/stream", s.handleLogsStream)
	mux.HandleFunc("/api/suggestions", s.handleSuggestions)
	mux.HandleFunc("/api/jobs/stream", s.handleJobsStream)
	mux.HandleFunc("/api/jobs/", s.handleJob)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/alerts/dismiss/", s.handleDismissAlert)
	mux.HandleFunc("/api/alerts/stream", s.handleAlertsStream)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/scheduler/run/", s.handleSchedulerRun)
	mux.HandleFunc("/api/scheduler/tasks", s.handleSchedulerTasks)

	// Static files and UI
	mux.Handle("/style.css", http.FileServer(http.Dir("./web")))
	mux.Handle("/app.js", http.FileServer(http.Dir("./web")))
	mux.Handle("/components/", http.StripPrefix("/components/", http.FileServer(http.Dir("./web/components"))))

	// Auth routes (registered unconditionally; handleLogin/handleLogout are
	// no-ops when auth is disabled because the middleware never blocks access)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// Jobs dedicated page
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./web/jobs.html")
	})

	// Root and fallback
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf(":%d", s.port)

	var handler http.Handler = mux
	if s.authEnabled {
		handler = authMiddleware(mux, s.sessionSecret)
		s.logger.Info("Auth enabled", zap.String("username", s.authUsername))
	}

	s.logger.Info("API server starting", zap.String("address", addr))
	go s.startAlertPoller()
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
			ID:                    t.ID,
			Title:                 t.FeedItem.Title,
			Size:                  t.FeedItem.Size,
			MatchReason:           t.MatchReason,
			Status:                t.Status,
			Link:                  t.FeedItem.Link,
			AIScore:               t.AIScore,
			AIReason:              t.AIReason,
			AIScored:              t.AIScored,
			MatchConfidence:       t.MatchConfidence,
			MatchConfidenceReason: t.MatchConfidenceReason,
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

	s.logBuffer.EmitAlertEvent(models.AlertRecord{
		Action:       "approve",
		TorrentID:    id,
		TorrentTitle: torrent.FeedItem.Title,
		MatchReason:  torrent.MatchReason,
		Message:      "Accepted: " + torrent.FeedItem.Title,
		TriggeredAt:  time.Now(),
	})

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

	s.logBuffer.EmitAlertEvent(models.AlertRecord{
		Action:       "queue",
		TorrentID:    id,
		TorrentTitle: torrent.FeedItem.Title,
		MatchReason:  torrent.MatchReason,
		Message:      "Queued for download: " + torrent.FeedItem.Title,
		TriggeredAt:  time.Now(),
	})

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

	s.logBuffer.EmitAlertEvent(models.AlertRecord{
		Action:       "reject",
		TorrentID:    id,
		TorrentTitle: torrent.FeedItem.Title,
		MatchReason:  torrent.MatchReason,
		Message:      "Rejected: " + torrent.FeedItem.Title,
		TriggeredAt:  time.Now(),
	})

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
// When a queue is available returns 202 {"job_id":N,"status":"queued"};
// otherwise executes synchronously and returns the full RescoreResponse.
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

	rescoreDeps := ops.RescoreDeps{
		Store:     s.store,
		Scorer:    s.scorer,
		Provider:  s.aiProvider,
		LogBuffer: s.logBuffer,
		Logger:    s.logger,
	}

	if s.queue != nil {
		jobID, err := s.store.CreateJob("rescore")
		if err == nil {
			now := time.Now()
			if s.logBuffer != nil {
				s.logBuffer.EmitJobEvent(models.JobRecord{
					ID:        jobID,
					Type:      "rescore",
					Status:    "running",
					StartedAt: now,
				})
			}
			s.registerJobCancel(jobID, "rescore")
			opts := ops.RescoreOptions{IDs: req.IDs, JobID: jobID, ProgressInterval: s.progressInterval}
			err = s.queue.Submit("rescore", true, func(ctx context.Context) {
				runCtx, runCancel := context.WithCancel(ctx)
				if !s.bindRunCancel(jobID, runCancel) {
					runCancel()
					return
				}
				defer runCancel()
				defer s.clearJobCancel(jobID)
				ops.RunRescore(runCtx, opts, rescoreDeps)
			})
			if err != nil {
				s.clearJobCancel(jobID)
				s.logger.Warn("failed to submit rescore job", zap.Error(err))
				_ = s.store.FailJob(jobID, err.Error())
				if s.logBuffer != nil {
					failedAt := time.Now()
					s.logBuffer.EmitJobEvent(models.JobRecord{
						ID:          jobID,
						Type:        "rescore",
						Status:      "failed",
						StartedAt:   now,
						CompletedAt: &failedAt,
						Summary:     models.JobSummary{ErrorMessage: err.Error()},
					})
				}
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
				return
			}
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(JobAcceptedResponse{JobID: jobID, Status: "queued"})
			return
		}
	}

	// Synchronous fallback (no queue or job allocation failed).
	updated, _ := ops.RunRescore(r.Context(), ops.RescoreOptions{IDs: req.IDs, ProgressInterval: s.progressInterval}, rescoreDeps)
	var responses []TorrentResponse
	for _, t := range updated {
		responses = append(responses, torrentToResponse(t))
	}
	json.NewEncoder(w).Encode(RescoreResponse{
		Rescored: len(responses),
		Torrents: responses,
	})
}

// handleRematch force re-runs parser+matcher on specific torrents and updates
// match_reason/status in place. Optional auto_rescore can re-run AI scoring on
// items that still match after rematch.
// POST /api/torrents/rematch  body: {"ids":[1,2,3],"auto_rescore":true}
// When a queue is available returns 202 {"job_id":N,"status":"queued"};
// otherwise executes synchronously and returns the full RematchResponse.
func (s *Server) handleRematch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	if s.matcher == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "matcher unavailable"})
		return
	}

	var req RematchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "request body must include at least one id"})
		return
	}

	rematchDeps := ops.RematchDeps{
		Store:     s.store,
		Matcher:   s.matcher,
		Enricher:  s.enricher,
		Scorer:    s.scorer,
		Provider:  s.aiProvider,
		LogBuffer: s.logBuffer,
		Logger:    s.logger,
	}

	if s.queue != nil {
		jobID, err := s.store.CreateJob("rematch")
		if err == nil {
			now := time.Now()
			if s.logBuffer != nil {
				s.logBuffer.EmitJobEvent(models.JobRecord{
					ID:        jobID,
					Type:      "rematch",
					Status:    "running",
					StartedAt: now,
				})
			}
			s.registerJobCancel(jobID, "rematch")
			opts := ops.RematchOptions{
				IDs:              req.IDs,
				AutoRescore:      req.AutoRescore,
				ForceAIEnrich:    req.ForceAIEnrich,
				JobID:            jobID,
				ProgressInterval: s.progressInterval,
			}
			err = s.queue.Submit("rematch", true, func(ctx context.Context) {
				runCtx, runCancel := context.WithCancel(ctx)
				if !s.bindRunCancel(jobID, runCancel) {
					runCancel()
					return
				}
				defer runCancel()
				defer s.clearJobCancel(jobID)
				ops.RunRematch(runCtx, opts, rematchDeps)
			})
			if err != nil {
				s.clearJobCancel(jobID)
				s.logger.Warn("failed to submit rematch job", zap.Error(err))
				_ = s.store.FailJob(jobID, err.Error())
				if s.logBuffer != nil {
					failedAt := time.Now()
					s.logBuffer.EmitJobEvent(models.JobRecord{
						ID:          jobID,
						Type:        "rematch",
						Status:      "failed",
						StartedAt:   now,
						CompletedAt: &failedAt,
						Summary:     models.JobSummary{ErrorMessage: err.Error()},
					})
				}
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
				return
			}
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(JobAcceptedResponse{JobID: jobID, Status: "queued"})
			return
		}
	}

	// Synchronous fallback (no queue or job allocation failed).
	result, _ := ops.RunRematch(r.Context(), ops.RematchOptions{
		IDs:              req.IDs,
		AutoRescore:      req.AutoRescore,
		ForceAIEnrich:    req.ForceAIEnrich,
		ProgressInterval: s.progressInterval,
	}, rematchDeps)
	var responses []TorrentResponse
	for _, t := range result.Updated {
		responses = append(responses, torrentToResponse(t))
	}
	json.NewEncoder(w).Encode(RematchResponse{
		Rematched:       result.Rematched,
		NoLongerMatches: result.NoLongerMatches,
		Rescored:        result.Rescored,
		Skipped:         result.Skipped,
		Torrents:        responses,
	})
}

func (s *Server) registerJobCancel(jobID int, jobType string) {
	s.jobCancelMu.Lock()
	defer s.jobCancelMu.Unlock()
	s.jobCancels[jobID] = &jobCancelState{jobType: jobType}
}

func (s *Server) bindRunCancel(jobID int, runCancel context.CancelFunc) bool {
	s.jobCancelMu.Lock()
	defer s.jobCancelMu.Unlock()
	st, ok := s.jobCancels[jobID]
	if !ok {
		return false
	}
	st.runCancel = runCancel
	if st.cancelRequested {
		runCancel()
	}
	return true
}

func (s *Server) requestJobCancel(jobID int) (ok bool, alreadyRequested bool, jobType string) {
	s.jobCancelMu.Lock()
	st, ok := s.jobCancels[jobID]
	if !ok {
		s.jobCancelMu.Unlock()
		return false, false, ""
	}
	alreadyRequested = st.cancelRequested
	st.cancelRequested = true
	jobType = st.jobType
	runCancel := st.runCancel
	s.jobCancelMu.Unlock()

	if runCancel != nil {
		runCancel()
	}
	return true, alreadyRequested, jobType
}

func (s *Server) clearJobCancel(jobID int) {
	s.jobCancelMu.Lock()
	defer s.jobCancelMu.Unlock()
	delete(s.jobCancels, jobID)
}

// handleJobs returns recent job records as JSON.
// GET /api/jobs?limit=50&status=<filter>
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	statusFilter := r.URL.Query().Get("status")

	jobs, err := s.store.ListJobs(limit, statusFilter)
	if err != nil {
		s.logger.Error("failed to list jobs", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []models.JobRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// handleJob returns a single job record by ID or accepts cancellation for a
// running async job.
// GET /api/jobs/{id}
// POST /api/jobs/{id}/cancel
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	isCancel := strings.HasSuffix(path, "/cancel")

	if isCancel {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path = strings.TrimSuffix(path, "/cancel")
		path = strings.TrimSuffix(path, "/")
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := path
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	job, err := s.store.GetJob(id)
	if err != nil {
		s.logger.Error("failed to get job", zap.Int("id", id), zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	if isCancel {
		if job.Status != "running" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "job is not running"})
			return
		}

		ok, _, jobType := s.requestJobCancel(id)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "job cannot be cancelled from this process"})
			return
		}

		if s.logBuffer != nil {
			s.logBuffer.EmitJobEvent(models.JobRecord{
				ID:        id,
				Type:      jobType,
				Status:    "running",
				StartedAt: job.StartedAt,
				Progress:  "cancel requested",
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(JobAcceptedResponse{JobID: id, Status: "cancelling"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// handleJobsStream streams live job lifecycle events over Server-Sent Events.
// GET /api/jobs/stream
func (s *Server) handleJobsStream(w http.ResponseWriter, r *http.Request) {
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

	ch, unsub := s.logBuffer.SubscribeJobs()
	defer unsub()

	fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	for {
		select {
		case job, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(job)
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

// handleAlerts returns buffered alert records as JSON (newest-last chronological order).
// POST /api/alerts/dismiss/{id}
func (s *Server) handleDismissAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/alerts/dismiss/")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		http.Error(w, "invalid alert id", http.StatusBadRequest)
		return
	}
	if !s.logBuffer.DismissAlert(id) {
		http.Error(w, "alert not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/alerts
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	alerts := s.logBuffer.RecentAlerts()
	if alerts == nil {
		alerts = []models.AlertRecord{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(alerts)
}

// handleAlertsStream streams live alert events over Server-Sent Events.
// GET /api/alerts/stream
//
// On connect the ring buffer is backfilled (recent alerts are replayed) then
// future events are forwarded as they arrive.
func (s *Server) handleAlertsStream(w http.ResponseWriter, r *http.Request) {
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

	ch, unsub := s.logBuffer.SubscribeAlerts()
	defer unsub()

	fmt.Fprintf(w, ": connected\n\n")
	fl.Flush()

	for {
		select {
		case alert, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(alert)
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

// startAlertPoller runs as a background goroutine for the lifetime of the
// server. It polls the jobs table on a 15-second ticker to detect events that
// originate from the cmdCheck OS process (which cannot call EmitAlertEvent
// directly because it shares no memory with cmdServe).
//
// Detected events:
//   - Any newly failed job (any type) → "job_failed" alert
//   - Newly completed feed_check job with ItemsMatched > 0 → "staged" alert
func (s *Server) startAlertPoller() {
	currentInterval := 15 * time.Second
	if s.settingsMgr != nil {
		if n := s.settingsMgr.Get().Alerts.AlertPollerIntervalSecs; n > 0 {
			currentInterval = time.Duration(n) * time.Second
		}
	}
	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	// Track the highest job ID we have already processed so we only surface new ones.
	lastSeenID := 0

	// Seed lastSeenID from the current latest job so we don't fire alerts for
	// history that existed before this server process started.
	if existing, err := s.store.ListJobs(1, ""); err == nil && len(existing) > 0 {
		lastSeenID = existing[0].ID
	}

	for range ticker.C {
		// Hot-reload alert poller interval from settings.
		if s.settingsMgr != nil {
			if n := s.settingsMgr.Get().Alerts.AlertPollerIntervalSecs; n > 0 {
				newInterval := time.Duration(n) * time.Second
				if newInterval != currentInterval {
					ticker.Reset(newInterval)
					currentInterval = newInterval
				}
			}
		}

		jobs, err := s.store.ListJobs(50, "")
		if err != nil {
			s.logger.Warn("alert poller: failed to list jobs", zap.Error(err))
			continue
		}

		for i := len(jobs) - 1; i >= 0; i-- { // iterate oldest → newest
			j := jobs[i]
			if j.ID <= lastSeenID {
				continue
			}
			// Only surface terminal states — skip still-running jobs.
			if j.Status == "running" {
				continue
			}

			switch {
			case j.Status == "failed":
				msg := "Job failed: " + j.Type
				if j.Summary.ErrorMessage != "" {
					msg += " — " + j.Summary.ErrorMessage
				}
				s.logBuffer.EmitAlertEvent(models.AlertRecord{
					Action:  "job_failed",
					Message: msg,
					TriggeredAt: func() time.Time {
						if j.CompletedAt != nil {
							return *j.CompletedAt
						}
						return time.Now()
					}(),
				})

			case j.Status == "cancelled":
				msg := j.Type + " cancelled"
				if j.Summary.ItemsMatched > 0 {
					msg += fmt.Sprintf(" (%d/%d processed)", j.Summary.ItemsMatched, j.Summary.ItemsFound)
				}
				s.logBuffer.EmitAlertEvent(models.AlertRecord{
					Action:  "job_cancelled",
					Message: msg,
					TriggeredAt: func() time.Time {
						if j.CompletedAt != nil {
							return *j.CompletedAt
						}
						return time.Now()
					}(),
				})

			case j.Status == "completed" && j.Type == "feed_check" && j.Summary.ItemsMatched > 0:
				s.logBuffer.EmitAlertEvent(models.AlertRecord{
					Action:  "staged",
					Message: fmt.Sprintf("Feed check: %d new match(es) staged", j.Summary.ItemsMatched),
					TriggeredAt: func() time.Time {
						if j.CompletedAt != nil {
							return *j.CompletedAt
						}
						return time.Now()
					}(),
				})

			case j.Status == "completed" && (j.Type == "rematch" || j.Type == "rescore" || j.Type == "rescore_backfill"):
				msg := j.Type + " completed"
				if j.Summary.ItemsFound > 0 {
					msg += fmt.Sprintf(" — %d/%d matched", j.Summary.ItemsMatched, j.Summary.ItemsFound)
				}
				s.logBuffer.EmitAlertEvent(models.AlertRecord{
					Action:  "job_completed",
					Message: msg,
					TriggeredAt: func() time.Time {
						if j.CompletedAt != nil {
							return *j.CompletedAt
						}
						return time.Now()
					}(),
				})
			}

			if j.ID > lastSeenID {
				lastSeenID = j.ID
			}
		}
	}
}

// applySettings pushes a settings snapshot into the live server state.
// It is called by WithSettings at startup and by handlePatchSettings after
// every successful update.
func (s *Server) applySettings(cfg settings.AppSettings) {
	// Progress interval
	if cfg.Alerts.ProgressInterval > 0 {
		s.progressInterval = cfg.Alerts.ProgressInterval
	}
	// Auth credentials
	if cfg.Auth.Username != "" {
		s.authUsername = cfg.Auth.Username
	}
	if cfg.Auth.Password != "" && cfg.Auth.Password != "***" {
		s.authPassword = cfg.Auth.Password
		s.authEnabled = true
	} else if cfg.Auth.Password == "" {
		// Explicit clear — disable auth
		s.authPassword = ""
		s.authEnabled = false
	}
	// Scheduler hot-reload
	if s.scheduler != nil {
		s.scheduler.SetInterval("feed_check",
			time.Duration(cfg.Scheduler.FeedCheckIntervalSecs)*time.Second)
		s.scheduler.SetEnabled("feed_check", cfg.Scheduler.FeedCheckEnabled)
		s.scheduler.SetEnabled("rescore_backfill", cfg.Scheduler.RescoreBackfillEnabled)
	}
	// Matcher default rules hot-reload
	if s.matcher != nil {
		s.matcher.SetDefaults(models.DefaultRules{
			MinQuality:      cfg.Match.MinQuality,
			PreferredCodec:  cfg.Match.PreferredCodec,
			ExcludeGroups:   cfg.Match.ExcludeGroups,
			PreferredGroups: cfg.Match.PreferredGroups,
		})
	}
}

// handleSettings serves GET /api/settings and PATCH /api/settings.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.settingsMgr == nil {
		http.Error(w, `{"error":"settings not configured"}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		cfg := s.settingsMgr.Get()
		// Mask password in response.
		if cfg.Auth.Password != "" {
			cfg.Auth.Password = "***"
		}
		json.NewEncoder(w).Encode(cfg)

	case http.MethodPatch:
		// Start from the current stored settings so omitted fields keep their values.
		current := s.settingsMgr.Get()
		if err := json.NewDecoder(r.Body).Decode(&current); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid JSON: " + err.Error()})
			return
		}
		if err := s.settingsMgr.Update(current); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
			return
		}
		s.applySettings(s.settingsMgr.Get())
		// Return updated settings with password masked.
		updated := s.settingsMgr.Get()
		if updated.Auth.Password != "" {
			updated.Auth.Password = "***"
		}
		json.NewEncoder(w).Encode(updated)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "method not allowed"})
	}
}

// handleSchedulerTasks returns a JSON array of all registered scheduler tasks
// and their current status.
func (s *Server) handleSchedulerTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.scheduler == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "scheduler not configured"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.scheduler.Status())
}

// handleSchedulerRun dispatches an on-demand execution of the named task.
// Returns 202 Accepted on success, 409 Conflict if the task is already running,
// or 404 if the task type is unknown.
func (s *Server) handleSchedulerRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	taskType := strings.TrimPrefix(r.URL.Path, "/api/scheduler/run/")
	if taskType == "" {
		http.Error(w, "task type required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if s.scheduler == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "scheduler not configured"})
		return
	}

	accepted := s.scheduler.RunNow(taskType)
	if !accepted {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(ErrorResponse{Error: "task already running or not found"})
		return
	}

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(SchedulerRunResponse{Status: "accepted", Type: taskType})
}
