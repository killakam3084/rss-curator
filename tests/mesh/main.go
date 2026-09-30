package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

type mesh struct {
	mu       sync.Mutex
	added    []string
	torrents []map[string]any
}

func main() {
	m := &mesh{}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", m.health)
	mux.HandleFunc("/rss/shows", m.rss)
	mux.HandleFunc("/rss/movies", m.movieRSS)
	mux.HandleFunc("/identity", m.plexIdentity)
	mux.HandleFunc("/library/sections", m.plexSections)
	mux.HandleFunc("/library/sections/1/all", m.plexShowsOrEpisodes)
	mux.HandleFunc("/library/sections/4/all", m.plexMovies)
	mux.HandleFunc("/library/metadata/17784", m.plexMobland)
	mux.HandleFunc("/library/metadata/17997", m.plexAmadeus)
	mux.HandleFunc("/api/v2/auth/login", m.qbitLogin)
	mux.HandleFunc("/api/v2/torrents/info", m.qbitInfo)
	mux.HandleFunc("/api/v2/torrents/add", m.qbitAdd)
	mux.HandleFunc("/api/tags", m.aiTags)
	mux.HandleFunc("/api/chat", m.aiChat)

	addr := ":32400"
	log.Printf("uat mesh listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (m *mesh) health(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	added := append([]string(nil), m.added...)
	m.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"status": "healthy", "added": added})
}

func (m *mesh) rss(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>UAT fixture</title>
<item><title>MobLand.S02E01.1080p.WEB-DL.x265-UAT</title><link>magnet:?xt=urn:btih:uat-mobland</link><guid>uat-mobland</guid><pubDate>Tue, 29 Sep 2026 12:00:00 GMT</pubDate><size>4000000000</size></item>
<item><title>Amadeus.1984.2160p.WEB-DL.x265-UAT</title><link>magnet:?xt=urn:btih:uat-amadeus</link><guid>uat-amadeus</guid><pubDate>Tue, 29 Sep 2026 12:01:00 GMT</pubDate><size>12000000000</size></item>
<item><title>Unowned.Show.S01E01.1080p.WEB-DL.x265-UAT</title><link>magnet:?xt=urn:btih:uat-unknown</link><guid>uat-unknown</guid><pubDate>Tue, 29 Sep 2026 12:02:00 GMT</pubDate><size>4000000000</size></item>
</channel></rss>`)
}

func (m *mesh) movieRSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>UAT movie fixture</title>
<item><title>Amadeus.1984.2160p.WEB-DL.x265-UAT</title><link>magnet:?xt=urn:btih:uat-amadeus</link><guid>uat-amadeus</guid><pubDate>Tue, 29 Sep 2026 12:01:00 GMT</pubDate><size>12000000000</size></item>
</channel></rss>`)
}

func (m *mesh) plexIdentity(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"machineIdentifier": "uat-mesh", "version": "uat-1.0"}})
}

func (m *mesh) plexSections(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Directory": []map[string]any{
		{"key": "4", "title": "Movies", "type": "movie"},
		{"key": "1", "title": "Television", "type": "show"},
	}}})
}

func (m *mesh) plexShowsOrEpisodes(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.URL.Query().Get("type") {
	case "2":
		writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Directory": []map[string]any{
			{"ratingKey": "17784", "title": "MobLand", "year": 2025, "type": "show"},
		}}})
	case "4":
		writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{
			{"ratingKey": "24256", "grandparentRatingKey": "17784", "title": "I Wanna Be Your Dog", "parentIndex": 2, "index": 1, "type": "episode", "Media": []map[string]any{{"videoResolution": "4k", "videoCodec": "hevc", "duration": 2934556, "Part": []map[string]any{{"size": 9351276799, "file": "/uat/MobLand.mkv"}}}}},
		}}})
	default:
		http.Error(w, "unsupported type", http.StatusBadRequest)
	}
}

func (m *mesh) plexMovies(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{
		{"ratingKey": "17997", "title": "Amadeus", "year": 1984, "type": "movie", "Media": []map[string]any{{"videoResolution": "1080", "videoCodec": "h264", "duration": 10825696, "Part": []map[string]any{{"size": 25890801371, "file": "/uat/Amadeus.mkv"}}}}},
	}}})
}

func (m *mesh) plexMobland(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"ratingKey": "17784", "title": "MobLand", "Guid": []map[string]string{{"id": "tvdb://446618"}, {"id": "tmdb://249042"}, {"id": "imdb://tt31566242"}}}}}})
}

func (m *mesh) plexAmadeus(w http.ResponseWriter, r *http.Request) {
	if !m.validToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"MediaContainer": map[string]any{"Metadata": []map[string]any{{"ratingKey": "17997", "title": "Amadeus", "Guid": []map[string]string{{"id": "imdb://tt0086879"}, {"id": "tmdb://279"}}}}}})
}

func (m *mesh) qbitLogin(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "Ok.") }
func (m *mesh) qbitInfo(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	writeJSON(w, http.StatusOK, m.torrents)
}
func (m *mesh) qbitAdd(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	values, _ := url.ParseQuery(string(body))
	link := values.Get("urls")
	m.mu.Lock()
	m.added = append(m.added, link)
	m.torrents = append(m.torrents, map[string]any{"hash": strconv.Itoa(len(m.torrents) + 1), "name": link, "state": "paused"})
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "Ok.")
}

func (m *mesh) aiTags(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]string{{"name": "uat-model"}}})
}
func (m *mesh) aiChat(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)
	content := `{"score":0.92,"reason":"deterministic UAT score","match_confidence":0.98,"match_confidence_reason":"fixture identity"}`
	if len(req.Messages) > 0 && strings.Contains(strings.ToLower(req.Messages[len(req.Messages)-1].Content), "metadata") {
		content = `{"show_name":"UAT Suggested Show","season":1,"episode":1,"quality":"1080P","codec":"x265","source":"WEB-DL","release_group":"UAT"}`
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": map[string]string{"role": "assistant", "content": content}})
}

func (m *mesh) validToken(r *http.Request) bool {
	return r.Header.Get("X-Plex-Token") == "uat-plex-token" || r.URL.Query().Get("X-Plex-Token") == "uat-plex-token"
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
