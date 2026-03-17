# RSS Curator — Project Summary

**Version:** v0.22.10 | **Language:** Go 1.22 | **Storage:** SQLite

A semi-automated torrent curator for private tracker RSS feeds with human-in-the-loop approval and optional AI-assisted scoring.

See [docs/ARCHITECTURE.md](./ARCHITECTURE.md) for system topology, state machine, and data model diagrams.

---

## Overview

RSS Curator runs on a configurable interval, fetches RSS feeds from private trackers, extracts structured metadata from torrent titles (show, season, episode, quality, codec, release group), matches items against a rule set, optionally scores them with an LLM, and stages matches for human review. A human approves or rejects via CLI or Web UI; approvals are forwarded to qBittorrent automatically.

The approve/reject history forms a training corpus that the AI Scorer uses to improve confidence estimates over time.

---

## Core Components

### 1. Feed Parser (`internal/feed/parser.go`)

- Fetches RSS feeds over HTTP using Go's standard library
- Extracts structured metadata from torrent titles via regex: `ShowName`, `Season`, `Episode`, `Quality`, `Codec`, `Source`, `ReleaseGroup`
- Optionally calls the AI Enricher when regex leaves `ShowName` or `Season` empty
- Stores all feed items (matched or not) to `raw_feed_items` with a 24-hour TTL for Web UI feed-stream visibility

### 2. Matcher (`internal/matcher/matcher.go`)

- Applies show/quality/codec/group rules to parsed feed items
- Two config modes:
  - **`shows.json`** (preferred): per-show rules with `DefaultRules` fallback — loaded from `./shows.json` or `~/.curator-shows.json`
  - **Environment variables** (legacy): `SHOW_NAMES`, `MIN_QUALITY`, `PREFERRED_CODEC`, `PREFERRED_GROUPS`, `EXCLUDE_GROUPS`
- `shows.json` takes precedence over env-var rules when present

### 3. AI Subsystem (`internal/ai/`)

Three components behind a pluggable `Provider` interface:

#### Provider (`provider.go`)

```
Provider interface:
  Complete(ctx, system, user string) (string, error)
  Available() bool

Implementations:
  ollamaProvider  — POST /api/chat on local Ollama instance (default)
  openAIProvider  — POST /v1/chat/completions (OpenAI-compatible)
  noopProvider    — silent no-op when CURATOR_AI_PROVIDER=disabled
```

Configured via environment variables:

| Variable | Default | Description |
|---|---|---|
| `CURATOR_AI_PROVIDER` | `ollama` | `ollama` · `openai` · `disabled` |
| `CURATOR_AI_HOST` | `http://localhost:11434` | LLM endpoint base URL |
| `CURATOR_AI_MODEL` | `llama3.2` | Model name for the provider |
| `CURATOR_AI_KEY` | *(empty)* | API key (OpenAI-compatible providers) |

#### Enricher (`enricher.go`)

- Fires only when regex metadata extraction leaves `ShowName == ""` or `Season == 0`
- Asks the LLM for `{show_name, season, episode, year}` as JSON
- Silent no-op on any error — never blocks the pipeline
- 15-second timeout per call

#### Scorer (`scorer.go`)

- Scores each staged torrent 0.0–1.0 for likelihood of human approval
- History context built via `BuildShowSummaries` (`internal/ai/history.go`) — compact per-show summaries (`+N -N | last:date | q=quality | ex=title...`) replace raw log lines; reduces token usage significantly and eliminates recency bias
- Candidate show summary is pinned first in history context; up to 3 other top-activity shows follow
- Prompt structure: history context (for context only) → candidate torrent → score instruction; model reads candidate immediately before task, eliminating positional confusion
- Uses Ollama structured output (`format: <JSON Schema>`) via `FormatSetter` interface — forces model to output exactly `score`, `reason`, `match_confidence`, `match_confidence_reason`; eliminates schema hallucination
- `num_ctx` and `num_predict` configurable via `CURATOR_AI_NUM_CTX` / `CURATOR_AI_NUM_PREDICT` env vars
- Emits `match_confidence` (0.0–1.0) and `match_confidence_reason` alongside `ai_score`
- Stores result as `ai_score` + `ai_reason` + `match_confidence` + `match_confidence_reason` on `StagedTorrent`
- Sets `ai_scored = true` even if score is `0.0` — disambiguates "never attempted" from "low confidence"
- Backfill runs on every `check` cycle: any `ai_scored=false` torrent across all statuses is re-scored
- Debug logs: `scorer.request` (includes `user_prompt` and `compressed_history`), `scorer.response` (includes `duration_ms`, `score`, `reason`, `match_confidence`, `raw_response`), `scorer.clamped` (out-of-range score clamped to 0.0–1.0)

#### History Aggregator (`history.go`)

- `BuildShowSummaries` aggregates `activity_log` entries into per-show `ShowSummary` structs
- Each summary tracks: approve/reject weighted counts, last action date, dominant quality, and a truncated example title
- `formatShortSummary` renders each show as a compact one-liner for prompt injection
- Candidate show is prioritised (included first regardless of activity rank); top 3 other shows by total activity weight follow

### 4. Log Buffer (`internal/logbuffer/buffer.go`)

In-memory ring buffer for structured application events; shared across all server subsystems via `*logbuffer.Buffer` wired at startup.

- **Log ring** — circular buffer of last N log entries; powers `/api/logs` and `/api/logs/stream` SSE
- **Jobs fan-out** — `EmitJobEvent` / `SubscribeJobs` — independent ring + subscriber map for job events; powers `/api/jobs/stream` SSE
- **Alerts fan-out** — `EmitAlertEvent` / `SubscribeAlerts` / `RecentAlerts` — ring (cap 50), subscriber map, ID counter; powers `/api/alerts` and `/api/alerts/stream` SSE
- New SSE clients receive full ring snapshot (backfill) before live events

### 5. Storage (`internal/storage/storage.go`)

SQLite database with four tables and idempotent `ALTER TABLE` migrations:

- **`staged_torrents`** — pending/approved/rejected torrent queue with AI score and match confidence columns
- **`activity_log`** — immutable record of human approve/reject decisions (Scorer training data)
- **`raw_feed_items`** — 24h sliding window of all RSS items seen (powers Feed Stream in Web UI)
- **`jobs`** — background task records: type (`feed_check` / `rescore` / `rescore_backfill`), status (`running` / `completed` / `failed`), start/end times, `summary_json` (items found/matched/scored/queued + error message)

Full schema and relationships: [docs/ARCHITECTURE.md — Data Model](./ARCHITECTURE.md#data-model)

### 6. HTTP API Server (`internal/api/server.go`)

Started with `curator serve`. Serves both the REST API and the Web UI static files.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/torrents?status=` | List torrents (default: `pending`) |
| `POST` | `/api/torrents/{id}/approve` | Approve → LogActivity + qBit add |
| `POST` | `/api/torrents/{id}/reject` | Reject → LogActivity |
| `POST` | `/api/torrents/rescore` | Trigger on-demand rescore of all pending torrents |
| `GET` | `/api/health` | Health check |
| `GET` | `/api/activity` | Approve/reject history with pagination |
| `GET` | `/api/stats` | 24h windowed counts: seen, staged, approved, rejected, queued, pending |
| `GET` | `/api/feed/stream` | Raw RSS feed items (last 24h, pre-filter) |
| `GET` | `/api/logs` | Buffered log entries as JSON; accepts `?since=<id>` |
| `GET` | `/api/logs/stream` | Live log stream via SSE (`text/event-stream`) |
| `GET` | `/api/jobs` | List all jobs (JSON) |
| `GET` | `/api/jobs/{id}` | Get single job by ID (JSON) |
| `GET` | `/api/jobs/stream` | Live job event stream via SSE |
| `GET` | `/api/alerts` | Ring snapshot of recent alerts (JSON) |
| `GET` | `/api/alerts/stream` | Live alert event stream via SSE (backfills ring on connect) |
| `GET` | `/` | Serves Web UI (`web/index.html`) |

**Background workers started by `cmdServe`:**
- `startAlertPoller` — 15s ticker reading the `jobs` table for new `failed` and `completed feed_check` jobs; emits `job_failed` and `staged` alerts bridging the `cmdCheck` → `cmdServe` process gap

Port configured via `CURATOR_API_PORT` (default `8081`). qBittorrent connection is optional at startup.

### 7. Web UI (`web/`)

Vue.js 3 single-page application served directly by the API server.

- Pending torrent queue with approve/reject buttons
- AI score badge `⚡ N%` on each card (shown when `ai_scored=true`; tooltip shows `ai_reason`)
- Low match-confidence badge `⚠ low confidence` (amber, with reason tooltip) when `match_confidence < 0.5`
- Torrents sorted by `ai_score` descending when any scores are present
- Activity log view, feed stream view
- Stats mini-panel in sidebar: 6 live tiles (pending + 5 × 24h windowed counts)
- Log drawer: DevTools-style bottom panel streaming live application logs over SSE; level badges (INFO/WARN/ERROR), text filter, auto-scroll toggle
- **Fixed top nav bar** — wordmark, jobs icon with animated badge (green pulse = running, red = failed), alerts bell with amber unread-count badge (shows "9+" above 9)
- **Jobs popover** — lists 5 most recent jobs with type, status dot, relative time, summary excerpt, "Go to Jobs →" link; mutually exclusive with alerts popover
- **Alerts popover** — lists 5 most recent alerts with action colour dot, message, relative time, match_reason when present; "clear" button; unread count tracked via `localStorage` key `rss-curator-alerts-read-at`
- `web/jobs.html` — standalone Jobs page: always-open SSE badge, three tabs (All / Active / Failed with live counts), expandable job rows
- Dark mode / light mode

### 8. qBittorrent Client (`internal/client/qbittorrent.go`)

Thin wrapper around the qBittorrent Web API: authenticate, add torrent by URL/magnet, pause/resume by title. Non-fatal on startup.

---

## CLI Commands

| Command | Description |
|---|---|
| `curator check` | Fetch feeds, match, score, stage new torrents; backfill unscored |
| `curator list [status]` | List staged torrents (default: `pending`) |
| `curator approve <id...>` | Approve by ID → qBit add |
| `curator reject <id...>` | Reject by ID |
| `curator review` | Interactive approve/reject/skip loop over pending queue |
| `curator serve` | Start HTTP API server + scheduler loop |
| `curator test` | Test qBittorrent and RSS feed connectivity |
| `curator resume <title>` | Resume qBittorrent torrent matching title |
| `curator pause <title>` | Pause qBittorrent torrent matching title |
| `curator cleanup [pattern...]` | Remove pending torrents matching SQL LIKE patterns (default: `%/t/%`) |
| `curator version` | Print version string |

---

## Project Structure

```
rss-curator/
├── cmd/curator/
│   └── main.go              CLI entry point, all command dispatch, cmdCheck pipeline
├── internal/
│   ├── ai/
│   │   ├── provider.go      Provider interface + Ollama, OpenAI, noop implementations; FormatSetter interface
│   │   ├── enricher.go      LLM metadata gap-fill (ShowName / Season fallback)
│   │   ├── scorer.go        Approval probability scorer with structured output + debug logging
│   │   └── history.go       ShowSummary aggregator for compact scorer history context
│   ├── api/
│   │   └── server.go        HTTP API server, handlers, jobs/alerts SSE, alert poller
│   ├── logbuffer/
│   │   └── buffer.go        In-memory ring buffer — logs, jobs fan-out, alerts fan-out
│   ├── client/
│   │   └── qbittorrent.go   qBittorrent Web API client
│   ├── feed/
│   │   └── parser.go        RSS fetch + regex metadata extraction
│   ├── matcher/
│   │   └── matcher.go       Rule-based matching (show/quality/codec/group)
│   └── storage/
│       └── storage.go       Store interface + SQLite implementation + migrations
├── pkg/models/
│   └── types.go             Shared types: FeedItem, StagedTorrent, Activity, ShowsConfig, JobRecord, JobSummary, AlertRecord
├── web/
│   ├── index.html           Vue.js dashboard
│   ├── jobs.html            Standalone Jobs page (Vue.js)
│   ├── app.js               Application logic
│   └── style.css            Tailwind-based styles
├── docs/
│   ├── ARCHITECTURE.md      System topology, state machine, ER diagram (Mermaid)
│   ├── PROJECT_SUMMARY.md   This file
│   ├── CONTAINER_GUIDE.md   Docker/Compose/GHCR reference
│   └── TRUENAS_DEPLOYMENT.md  TrueNAS SCALE deploy guide
├── scripts/
│   ├── start.sh             Container entrypoint (scheduler + serve dual-mode)
│   └── scheduler.sh         Cron-style check loop
├── shows.json.sample        Example per-show matching config
├── curator.env.sample       Example environment variable config
├── docker-compose.yml       Production Compose config
├── compose.dev.yml          Development Compose config (with hot reload)
├── Dockerfile
├── Makefile
├── go.mod
├── README.md
├── CHANGELOG.md
└── START_HERE.md
```

---

## Dependencies

| Package | Purpose |
|---|---|
| [`autobrr/go-qbittorrent`](https://github.com/autobrr/go-qbittorrent) | qBittorrent Web API client |
| [`mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3) | SQLite driver (CGO) |
| [`go.uber.org/zap`](https://github.com/uber-go/zap) | Structured logging in API server and AI subsystem |

---

## Design Principles

- **Minimal dependencies** — 3 external packages; standard library for HTTP, JSON, RSS parsing
- **Idempotent operations** — `INSERT OR IGNORE` on deduplication key; `ALTER TABLE IF NOT EXISTS` migrations; safe to re-run `check` at any time
- **Graceful degradation** — AI provider unavailable: pipeline continues rule-based. qBittorrent unavailable: approval queue still serves
- **Clean training signal** — `activity_log` records only human curation intent, never operational download state; Scorer reads pure signal
- **Interface-driven** — `storage.Store` and `ai.Provider` are interfaces; tests use mock implementations; implementations are swappable

---

## Roadmap

- [x] **Scorer `match_confidence` signal** — scorer returns `match_confidence` (0.0–1.0) and `match_confidence_reason`; UI shows amber ⚠ `low confidence` badge with tooltip when `match_confidence < 0.5`
- [x] **Jobs system** — background task tracking for `feed_check`, `rescore`, `rescore_backfill`; jobs SSE fan-out; standalone `/jobs` page
- [x] **Alerts system** — ephemeral in-memory ring (cap 50); approve/reject/queue/staged/job_failed alert types; bell UI with unread badge; SSE fan-out with ring backfill; unread persistence via `localStorage`
- [x] **Compact show-history summaries** — `BuildShowSummaries` aggregator for token-efficient scorer history; eliminates recency bias
- [x] **Ollama structured output** — `FormatSetter` interface; JSON Schema enforcement eliminating schema hallucination; `num_ctx`/`num_predict` configurable
- [ ] YAML configuration file support
- [ ] Webhook notifications (on approve/stage)
- [ ] Season pack handling
- [ ] Multi-tracker aggregate feed support
- [ ] Custom metadata extraction patterns
- [ ] **Candidate-focused retrieval** — embeddings or fuzzy matching to select the most relevant history summaries for a candidate at score time
- [ ] **App-level Prometheus metrics** — expose `/metrics` endpoint; track scorer latency, error rate, clamp frequency, model/algorithm version
- [ ] **Suggester engine** — analyse accept/reject history to infer franchise/genre/creator patterns; surface suggested `shows.json` rules proactively before matching episodes appear in the feed
