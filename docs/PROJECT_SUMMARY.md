# RSS Curator — Project Summary

**Version:** v0.14.2 | **Language:** Go 1.22 | **Storage:** SQLite

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
- Builds context from the last 20 `activity_log` entries (`[APPROVED] Title` / `[REJECTED] Title`)
- Stores result as `ai_score` + `ai_reason` (≤80 chars) on `StagedTorrent`
- Sets `ai_scored = true` even if the returned score is `0.0` — disambiguates "never attempted" from "low confidence"
- Backfill runs on every `check` cycle: any `ai_scored=false` torrent across all statuses is re-scored

### 4. Storage (`internal/storage/storage.go`)

SQLite database with three tables and idempotent `ALTER TABLE` migrations:

- **`staged_torrents`** — pending/approved/rejected torrent queue with AI score columns
- **`activity_log`** — immutable record of human approve/reject decisions (Scorer training data)
- **`raw_feed_items`** — 24h sliding window of all RSS items seen (powers Feed Stream in Web UI)

Full schema and relationships: [docs/ARCHITECTURE.md — Data Model](./ARCHITECTURE.md#data-model)

### 5. HTTP API Server (`internal/api/server.go`)

Started with `curator serve`. Serves both the REST API and the Web UI static files.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/torrents?status=` | List torrents (default: `pending`) |
| `POST` | `/api/torrents/{id}/approve` | Approve → LogActivity + qBit add |
| `POST` | `/api/torrents/{id}/reject` | Reject → LogActivity |
| `GET` | `/api/health` | Health check |
| `GET` | `/api/activity` | Approve/reject history with pagination |
| `GET` | `/api/stats` | 24h windowed counts: seen, staged, approved, rejected, queued, pending |
| `GET` | `/api/feed/stream` | Raw RSS feed items (last 24h, pre-filter) |
| `GET` | `/api/logs` | Buffered log entries as JSON; accepts `?since=<id>` |
| `GET` | `/api/logs/stream` | Live log stream via Server-Sent Events (`text/event-stream`) |
| `GET` | `/` | Serves Web UI (`web/index.html`) |

Port configured via `CURATOR_API_PORT` (default `8081`). qBittorrent connection is optional at startup.

### 6. Web UI (`web/`)

Vue.js 3 single-page application served directly by the API server.

- Pending torrent queue with approve/reject buttons
- AI score badge `⚡ N%` on each card (shown when `ai_scored=true`; tooltip shows `ai_reason`)
- Torrents sorted by `ai_score` descending when any scores are present
- Activity log view, feed stream view
- Stats mini-panel in sidebar: 6 live tiles (pending + 5 × 24h windowed counts)
- Log drawer: DevTools-style bottom panel streaming live application logs over SSE;
  level badges (INFO/WARN/ERROR), text filter, auto-scroll toggle
- Dark mode / light mode

### 7. qBittorrent Client (`internal/client/qbittorrent.go`)

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
│   │   ├── provider.go      Provider interface + Ollama, OpenAI, noop implementations
│   │   ├── enricher.go      LLM metadata gap-fill (ShowName / Season fallback)
│   │   └── scorer.go        Approval probability scorer (0.0–1.0)
│   ├── api/
│   │   └── server.go        HTTP API server, handlers, JSON response types
│   ├── client/
│   │   └── qbittorrent.go   qBittorrent Web API client
│   ├── feed/
│   │   └── parser.go        RSS fetch + regex metadata extraction
│   ├── matcher/
│   │   └── matcher.go       Rule-based matching (show/quality/codec/group)
│   └── storage/
│       └── storage.go       Store interface + SQLite implementation + migrations
├── pkg/models/
│   └── types.go             Shared types: FeedItem, StagedTorrent, Activity, ShowsConfig
├── web/
│   ├── index.html           Vue.js dashboard
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
| [`go.uber.org/zap`](https://github.com/uber-go/zap) | Structured logging in API server |

---

## Design Principles

- **Minimal dependencies** — 3 external packages; standard library for HTTP, JSON, RSS parsing
- **Idempotent operations** — `INSERT OR IGNORE` on deduplication key; `ALTER TABLE IF NOT EXISTS` migrations; safe to re-run `check` at any time
- **Graceful degradation** — AI provider unavailable: pipeline continues rule-based. qBittorrent unavailable: approval queue still serves
- **Clean training signal** — `activity_log` records only human curation intent, never operational download state; Scorer reads pure signal
- **Interface-driven** — `storage.Store` and `ai.Provider` are interfaces; tests use mock implementations; implementations are swappable

---

## Roadmap

- [ ] YAML configuration file support
- [ ] Webhook notifications (on approve/stage)
- [ ] Season pack handling
- [ ] Multi-tracker aggregate feed support
- [ ] Custom metadata extraction patterns
- [ ] **Scorer match-confidence signal** — currently the scorer is instructed to treat the matcher's match reason as authoritative and not re-evaluate it; this creates a blind spot where a structurally valid match (rule fired, quality met, preferred group present) is semantically wrong (e.g. a broad substring rule firing on an unrelated title)
  - Add a separate `match_confidence` output field to the scorer response, distinct from `score`; the scorer assesses whether the matched rule plausibly describes the actual content, without overriding release-quality scoring
  - Low `match_confidence` can surface as a UI warning, suppress auto-queue, or route to a dedicated review state — the response to it is a product decision, not baked into the scorer
  - Keeps the scorer's role general: any rule-vs-title divergence (substring collision, regex too broad, franchise spin-off, franchise reboot with shared keywords) benefits from the same signal
- [ ] **Suggester engine** (`POST /api/suggestions` stub already in place)
  - Analyse accept/reject history to infer franchise, genre, and creator patterns
  - Surface suggested `shows.json` rules *proactively* — before matching episodes ever appear in the feed
  - Distinguish between two rule-generation modes:
    - **Exact-show rule** — new title clearly not covered by any existing rule (e.g. a spin-off whose name doesn't overlap with the parent)
    - **Franchise broadening** — an existing rule is catching a spin-off via loose substring match (e.g. `Yellowstone` matching `Marshals: A Yellowstone Story`); suggest a dedicated rule for the spin-off rather than widening the parent regex
  - Rank suggestions by confidence; include a human-readable rationale for each so the user can accept or dismiss with context
