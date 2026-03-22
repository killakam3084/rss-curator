# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [0.27.0] - 2026-03-22

### Added
- **Live job progress** — rematch and rescore background jobs now emit mid-loop SSE progress events (`"progress": "47 / 367"`) on the first item and every 10 items thereafter.
- `JobRecord` gains a `progress` field (`json:"progress,omitempty"`) — set only on running events, never persisted to SQLite.
- Job notification strip in the dashboard updates in real time as progress events arrive; falls back to "starting…" until the first event.
- Running rows in the Jobs popover (navbar) also show the live progress string.


## [0.26.0] - 2026-03-22

### Added
- Hurl-based E2E functional validation suite (`tests/e2e/`) covering all stable API endpoints: health, stats, activity, jobs, torrents, feed stream, scheduler (task list + on-demand run + unknown-type conflict), rescore/rematch validation errors, and alerts.
- `tests/e2e/auth/auth-flow.hurl`: standalone session-cookie auth-flow test — unauthenticated 401, bad-credential redirect, correct-credential login+cookie capture, authenticated request.
- `docker-compose.test.yml`: CI test stack — builds a fresh curator image with an ephemeral SQLite DB and no auth; Hurl container waits on healthcheck then runs the full smoke suite.
- `docker-compose.validate.yml`: TrueNAS/live-stack sidecar — host-network Hurl container that runs the smoke suite (with cookie jar) then the auth-flow test against an already-running curator instance; credentials injected via `CURATOR_USERNAME` / `CURATOR_PASSWORD` shell env.
- `.github/workflows/e2e.yml`: GitHub Actions workflow that runs `make test-e2e` on every push/PR to `main`.
- `Makefile`: `test-e2e` (CI; builds + runs + tears down) and `validate-smoke` (TrueNAS live) targets.


## [0.25.0] - 2026-03-22

### Added
- `internal/scheduler`: new in-process scheduler package — registers named `Task` values with configurable intervals; one goroutine per task; atomic CAS deduplication ensures a task cannot overlap itself; `RunNow`, `SetEnabled`, `SetInterval` for runtime control.
- `internal/jobs`: new async job queue package — single worker, dual-lane priority (cap 5 high / cap 50 normal), per-type deduplication via `Submit`/`ErrAlreadyActive`.
- `GET /api/scheduler/tasks`: returns JSON snapshot of all registered tasks with type, interval, enabled, running, last_run, next_run.
- `POST /api/scheduler/run/{type}`: on-demand task dispatch; 202 Accepted on success, 409 Conflict if already running or task unknown.
- `feed_check` task auto-registered at serve startup with interval from `CHECK_INTERVAL` env var (defaults 3600 s); replaces reliance on external `scheduler.sh` cron.
- `Server.WithScheduler` / `Server.WithQueue` builder methods for attaching scheduler and queue without changing `NewServer` signature.
- `internal/ops.RescoreOptions.JobID` / `RematchOptions.JobID`: callers may pre-allocate a job record and the function skips re-creating it.

### Changed
- `POST /api/torrents/rescore` and `POST /api/torrents/rematch` now return **202 Accepted** `{"job_id": N, "status": "queued"}` when the server has a job queue wired in. The synchronous 200 path is retained as a fallback when no queue is configured.
- Frontend (`app.js`) rescore and rematch handlers check `response.status === 202` and show a queued toast with the job ID, directing users to the Jobs log for progress.


## [0.24.2] - 2026-03-19

### Added
- Startup config logs now explicitly report when `shows.json` could not be loaded and the app is falling back to legacy environment-variable matching rules.
- Regression tests added for CSV env parsing and legacy matcher behavior around empty watchlists/rule names.

### Fixed
- Legacy matching no longer treats an unset `SHOW_NAMES` value as a wildcard that matches every feed item.
- Empty/whitespace entries in `SHOW_NAMES`, `EXCLUDE_GROUPS`, and `PREFERRED_GROUPS` are now trimmed out during config parsing.


## [0.24.1] - 2026-03-16

### Added
- Rematch UI now supports `force_ai_enrich` as an explicit option, allowing AI title enrichment to override already-parsed fields before matcher evaluation.

### Changed
- Rematch API accepts `force_ai_enrich` and uses forced enricher mode when requested, making ambiguous-title diagnostics and AI-assisted re-evaluation easier from the dashboard.


## [0.24.0] - 2026-03-16

### Added
- Batch re-match capability across `pending`, `accepted`, and `rejected` torrents via new API endpoint `POST /api/torrents/rematch`, including job tracking and job-stream updates.
- Web UI re-match actions for single and multi-select workflows, plus a rematch modal with `auto_rescore` toggle so parser/matcher remediation and scoring refresh can be run together.
- Server-side rematch pipeline now re-runs title parsing (current parser rules), optional AI enrichment, matcher evaluation, and in-place persistence of refreshed `feed_item` + `match_reason`.

### Changed
- `serve` wiring now injects matcher and enricher dependencies into the API server so rematch uses the same matching stack as feed checks.

### Fixed
- Aggressive cleanup path: torrents that no longer match current rules during rematch are reconciled to `rejected` with explicit rematch context in `match_reason`.
- Rematch persistence now clears stale AI scoring/confidence fields when match context changes, preventing outdated score display.


## [0.23.1] - 2026-03-16

### Added
- Enricher fallback now supports additional structured fields (`quality`, `codec`, `source`, `release_group`) and backfills missing values conservatively (only when parser fields are empty).

### Fixed
- Feed parser codec extraction now handles space-separated forms like `H 265` / `H 264` in addition to existing `x265`/`x264`/`H.265`/`HEVC` patterns.


## [0.23.0] - 2026-03-16

### Added
- Pluggable TV metadata provider subsystem (`internal/metadata/`): provider interface + factory, TVMaze live implementation (free, no API key), TMDB and TVDB stubs ready for future implementation, and a noop/disabled provider.
- SQLite-backed TTL cache (`internal/metadata/cache.go`) co-located with the main `curator.db` — placed in the same directory automatically so it lands on the correct container volume or dataset without extra configuration. Explicit override available via `CURATOR_META_DB`.
- Cache-first lookup resolver (`internal/metadata/lookup.go`) with configurable TTL (`CURATOR_META_TTL_HOURS`, default 7 days). `Resolve` never returns an error — metadata is always additive and never blocks normal operation.
- AI scorer now enriches each candidate's prompt with TV metadata (genres, network, status, premiere year) when available, giving the LLM richer context for show-type disambiguation and approval likelihood.
- New env vars documented in `curator.env.sample`: `CURATOR_META_PROVIDER`, `CURATOR_META_KEY`, `CURATOR_META_HOST`, `CURATOR_META_TTL_HOURS`, `CURATOR_META_DB`.
- Removed duplicate `scorer.request` debug log emission that was introduced in v0.22.10.


## [0.22.11] - 2026-03-16

### Changed
- Documentation refresh across `PROJECT_SUMMARY.md`, `ARCHITECTURE.md`, `FIELD_NOTES.md`, and `README.md` to reflect the v0.22.x series.

## [0.22.10] - 2026-03-16

### Added
- Log compressed show-history context in `scorer.request` debug logs for benchmarking and validation. This makes it easy to correlate the prompt input with each scorer response.

## [0.22.9] - 2026-03-16

### Added
- Compact show-history summaries for the AI scorer to reduce token usage and improve scoring stability; introduces `internal/ai/history.go` and updates `internal/ai/scorer.go` to use summary-based history.

## [0.22.8] - 2026-03-15

### Changed
- Alerts system: UI polish and bugfixes for popover mutual exclusion, active-state styling, and transitions in `web/index.html` and `web/app.js`.
- Alerts popover and jobs popover are now mutually exclusive (opening one closes the other).
- Popover buttons show persistent active state while open.
- Added Vue `<Transition>` fade/slide for popover open/close.
- Overlay closes both popovers on outside click.

### Fixed
- Minor UI/UX bugs in alerts and jobs popovers.

## [0.22.7] - 2026-03-15

### Fixed
- AI scorer recency bias: model was reading the last history entries (e.g. `[REJECT] Beachfront Bargain Hunt Renovation`) immediately before the task instruction and anchoring on them as the subject to score rather than the candidate torrent
- Restructured user prompt from `[candidate] → [history] → score` to `[history (context)] → [candidate] → score` — candidate torrent now appears immediately before the task instruction so it is the last thing the model reads, eliminating positional confusion
- Added explicit label `"Recent approval history (for context only — do not score these):"` before history block and `"Candidate torrent to score:"` before the release fields
- Rewrote system prompt to match new prompt structure: numbered inputs (1. history, 2. candidate), explicit `"Score ONLY the candidate torrent"` rule, consolidated into cleaner instruction set

## [0.22.6] - 2026-03-15

### Fixed
- AI scorer producing nonsensical reasons like `"Highly similar to a well-formed JSON object"` — llama3.2 was treating the user prompt text itself as the thing to evaluate rather than the torrent described within it
- Added explicit task-closing instruction `"Score the torrent release described above."` at the end of every user prompt, anchoring the model's task after it has received all context
- Removed `max 80 chars` hints from system prompt field descriptions — structured output enforces types not lengths; the hints were adding noise that contributed to prompt confusion on smaller models

## [0.22.5] - 2026-03-15

### Fixed
- AI scorer hallucinating a JSON Schema definition (`{"title":"Approved","type":"object","properties":{...}}`) instead of a score response — root cause: `format: "json"` (loose JSON mode) causes llama3.2 to infer and output a schema rather than populate one
- Replaced loose `format: "json"` with Ollama structured outputs: the scorer now sends `format: {JSON Schema}` pinning the model to exactly `score`, `reason`, `match_confidence`, `match_confidence_reason` — the model is forced to populate those four fields and cannot generate extra keys or off-schema structures
- Added `FormatSetter` interface to `ai.Provider` layer so the scorer can configure structured output without the `Provider` interface signature changing
- `scoreOutputSchema` defined as a package-level `json.RawMessage` in `scorer.go`; applied via `configureFormat()` at `NewScorer` construction time via type assertion

## [0.22.4] - 2026-03-15

### Fixed
- AI scorer response `unexpected end of JSON input` — root cause was two compounding issues:
  1. `num_predict=200` was too low; llama3.2 was generating a verbose wrong-schema response that exceeded the cap and produced truncated JSON. Default raised to `400`
  2. Ollama was not in JSON mode — added `format: "json"` to every `/api/chat` request, which constrains the model to syntactically valid JSON output regardless of verbosity
- Added explicit JSON shape example to the scorer system prompt to reduce schema hallucination (`status`/`title`/`url` keys instead of `score`/`reason`) on smaller models

## [0.22.3] - 2026-03-13

### Changed
- Ollama provider now sends `num_ctx` and `num_predict` in every `/api/chat` request
- `CURATOR_AI_NUM_CTX` (default `2048`) — caps the KV cache context window; eliminates Ollama's wasteful 128K default allocation for curator's ~400-token prompts
- `CURATOR_AI_NUM_PREDICT` (default `200`) — caps token generation; scorer output is 60-80 tokens of JSON, so this prevents runaway generation without any risk of truncation
- Both vars documented in `curator.env.sample`

## [0.22.2] - 2026-03-13

### Changed
- Jobs and alerts popovers are now mutually exclusive — opening one closes the other; both can no longer be stacked simultaneously

## [0.22.1] - 2026-03-13

### Changed
- Jobs and alerts popover buttons now show a persistent `bg-gray-800 border-gray-600` active state while their popover is open, making focus visually unambiguous
- Both popovers gain a Vue `<Transition>` fade + slide-down entrance (150 ms ease-out) and slide-up exit (100 ms ease-in), so open/close feel intentional rather than an instant snap

## [0.22.0] - 2026-03-13

### Added
- **Alerts system** — ephemeral in-memory notification ring (cap 50) for curator actions: `approve`, `reject`, `queue`, `staged` (feed_check with matched items), and `job_failed`
- `AlertRecord` struct in `pkg/models/types.go` — `id`, `action`, `torrent_id`, `torrent_title`, `match_reason`, `message`, `triggered_at`
- `logbuffer.Buffer` gains `EmitAlertEvent`, `SubscribeAlerts` (backfills ring on connect), and `RecentAlerts` — parallel to the existing jobs fan-out, using an independent ring, mutex, and subscriber map
- **REST endpoints**: `GET /api/alerts` (JSON ring snapshot), `GET /api/alerts/stream` (SSE — replays ring backfill then streams live events)
- **Background alert poller** (`startAlertPoller`) — 15 s ticker in `cmdServe` reads the SQLite `jobs` table for new `failed` jobs and `completed` `feed_check` jobs with matches; bridges the `cmdCheck`→`cmdServe` process gap; seeds `lastSeenID` on startup to avoid surfacing pre-existing history
- **Alert bell icon** in the fixed top nav — amber `bg-amber-400` unread-count badge (shows "9+" above 9); clicking opens the alerts popover and clears the unread count
- **Alerts popover** — `w-80` dropdown listing the 5 most recent alerts; action colour dot (emerald=approve, red=reject/job_failed, blue=queue, curator-500=staged); message, relative time, and `match_reason` when present; "clear" button purges client-side list; outside-click overlay closes both jobs and alerts popovers
- Unread tracking via `localStorage` key `rss-curator-alerts-read-at` (ISO timestamp); survives page reload

### Changed
- `handleApprove`, `handleQueue`, `handleReject` each emit an `AlertRecord` with relevant torrent metadata after logging activity
- `handleRescore` failure path emits a `job_failed` alert
- Outside-click overlay now closes both `jobsPopoverOpen` and `alertsPopoverOpen`
- `app.js`: adds `alerts`, `alertsPopoverOpen`, `lastReadAt` refs; `unreadAlerts` and `recentAlerts` computed; `fetchAlerts`, `openAlertsStream` (always-open SSE with upsert + auto-reconnect), `markAlertsRead`, `clearAlerts` helpers; all called/returned appropriately
- `server_test.go`: test `Server` now wires a real `logbuffer.NewBuffer()` instead of `nil` so `EmitAlertEvent` calls in handlers don't panic during unit tests

## [0.21.0] - 2026-03-12

### Added
- **Jobs system** — background task tracking across `cmdCheck`, `handleRescore`, and the backfill scorer; every run creates a `jobs` row with type, status, start/end times, and a `JobSummary` (items found / matched / scored / queued + error message)
- **Job types**: `feed_check` (CLI `check`/`scan`), `rescore` (POST `/api/torrents/rescore`), `rescore_backfill` (automatic backfill block inside `cmdCheck`)
- **SQLite Migration 6** — `jobs` table: `id`, `type`, `status` (`running` / `completed` / `failed`), `started_at`, `completed_at`, `summary_json`
- **Storage API** — `CreateJob`, `CompleteJob`, `FailJob`, `ListJobs`, `GetJob` methods on `*Storage` and `Store` interface
- **Jobs SSE fan-out** — `logbuffer.Buffer` gains `EmitJobEvent(models.JobRecord)` + `SubscribeJobs()` (parallel map, independent of log SSE)
- **REST endpoints**: `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/jobs/stream` (SSE)
- **`/jobs` page** — standalone Vue 3 dark-mode app (`web/jobs.html`): breadcrumb back to curator, always-open SSE badge, three tabs (All / Active / Failed with live counts), expandable rows with type badge, status dot (pulsing while running), relative start time, duration, and full summary stats
- **Fixed top navigation bar** — replaces the scrolling header card in `index.html`; `h-14` bar spans from left edge to sidebar edge (tracks `sidebarCollapsed` reactive binding); contains wordmark, briefcase jobs icon with animated badge (green pulse = running, red = failed), alerts placeholder (future), and logout button
- **Jobs popover** — clicking the jobs icon opens a compact popover listing the 5 most recent jobs with type, status dot, relative time, summary excerpt, and a "Go to Jobs →" link to `/jobs`; outside-click overlay closes it
- **Sidebar top offset** — sidebar now starts at `top-14` (below the nav bar) instead of `top-0`; height changed to `calc(100vh - 3.5rem)`
- `JobRecord` and `JobSummary` structs in `pkg/models/types.go`

### Changed
- `cmdCheck` instruments the feed-check loop with a `feed_check` job; tracks `ItemsFound`, `ItemsMatched`, `ItemsScored`; marks the job completed or failed at exit; backfill block creates a separate `rescore_backfill` job
- `handleRescore` wraps scoring with a `rescore` job and calls `logBuffer.EmitJobEvent` on create/complete/fail so the UI badge updates in real time
- Main content area gains `paddingTop: 56px` so it clears the fixed nav bar
- Toast notifications offset adjusted to `top-20` to clear the new nav bar
- `app.js` version: adds `jobs`, `jobsPopoverOpen` refs; `runningJobs`, `failedJobs`, `recentJobs` computed; `fetchJobs` (initial backfill) + `openJobsStream` (always-open SSE with upsert-by-ID and auto-reconnect) called on `onMounted`; `formatRelative` helper exposed to template

### Tests
- `TestCreateJob`, `TestCompleteJob`, `TestFailJob`, `TestListJobs`, `TestGetJob_NotFound` added to `internal/storage/storage_test.go`
- `mockStorage` in `internal/api/server_test.go` extended with the five new `Store` interface methods

## [0.20.3] - 2026-03-11

### Fixed
- **`match_confidence` prompt disambiguation** — scorer user prompt now surfaces `Matched rule` (the rule name extracted from match reason, e.g. `NOVA`) and `Parsed show (from title)` (the feed-parsed content name, e.g. `Beachfront Bargain Hunt Renovation`) as separate labeled fields; previously both were collapsed under a single ambiguous `Show:` label, causing the model to inconsistently anchor on either field and produce divergent confidence scores for identical release patterns
- `extractMatchedRule()` helper parses `"matches show: NAME"` from the match reason string; falls back to the full reason for quality/group-only matches
- System prompt updated to reference `"Matched rule"` and `"Parsed show"` by their correct labels

## [0.20.2] - 2026-03-11

### Added
- **`match_confidence` scorer signal** — scorer now returns two additional fields: `match_confidence` (0.0–1.0, sentinel -1 = not yet assessed) and `match_confidence_reason` (one-line explanation); orthogonal to `ai_score` — the scorer evaluates whether the matched rule name plausibly identifies the actual content in the title, independent of release quality
- **Low-confidence UI badge** — torrent cards show an amber ⚠ `low confidence` badge (with reason tooltip) when `match_confidence >= 0 && match_confidence < 0.5`; complements the existing ⚡ AI score badge
- `StagedTorrent.MatchConfidence` and `StagedTorrent.MatchConfidenceReason` fields in `pkg/models/types.go`
- SQLite migrations 4 and 5: `match_confidence REAL DEFAULT -1` and `match_confidence_reason TEXT DEFAULT ''`; idempotent log-and-continue pattern
- `TorrentResponse.MatchConfidence` / `TorrentResponse.MatchConfidenceReason` exposed on all API list and rescore endpoints

### Changed
- `Store.UpdateAIScore` signature extended: `(id int, score float64, reason string, confidence float64, confidenceReason string) error`
- Scorer system prompt updated to request `match_confidence` and `match_confidence_reason` fields with semantic guidance (e.g. rule name vs incidental substring distinction)
- `match_confidence` clamped to [0, 1] post-parse; -1 sentinel preserved when model omits the field

## [0.20.1] - 2026-03-11

### Fixed
- **Matcher word-boundary matching** — show name rules now use `\b<name>\b` regex instead of `strings.Contains`; prevents embedded-substring false positives such as `NOVA` matching `Renovation` or `Invincible` matching `The Invincible Samurai`; applies to both `matchWithShowsConfig` and the legacy `matchesShowName` path; `regexp.QuoteMeta` ensures rule names with special characters (e.g. `Mr. Robot`) compile safely with graceful fallback to contains on error
- **Scorer temperature = 0** — ollama and openai providers now carry a `temperature float64` field; `NewProviderFor("scorer")` sets `temperature=0` (deterministic argmax output); all other subsystems (enricher, suggester) use `temperature=1`; eliminates the non-deterministic scoring variance observed on identical release profiles (e.g. S12E02 scoring 0.2 vs S12E03 scoring 0.8 for the same show/quality/codec/group)

## [0.20.0] - 2026-03-10

### Added
- **In-app authentication** — set `CURATOR_PASSWORD` to enable a branded login page at `/login`; unset leaves the app open for local dev (backward-compatible)
- `internal/api/auth.go` — HMAC-SHA256 signed session cookies (`curator_session`); `authMiddleware` wraps the mux when auth is enabled; unauthenticated `/api/*` returns `401 JSON`, UI paths redirect to `/login`
- `GET /login` serves `web/login.html`; `POST /login` validates credentials (constant-time compare) and issues a `HttpOnly; SameSite=Strict` session cookie
- `POST /logout` clears the session cookie and redirects to `/login`
- `web/login.html` — on-brand login page: dark `bg-gray-950`, triangle SVG background, curator-green wordmark, mono font, error banner on `?error=1`; no Vue/JS framework, plain HTML form
- Logout button in the app header (quiet `text-gray-500 hover:text-red-400` style, `POST /logout` form)
- New env vars: `CURATOR_USERNAME` (default `curator`), `CURATOR_PASSWORD`, `CURATOR_SESSION_SECRET`, `CURATOR_SESSION_TTL_HOURS` (default `24`); all documented in `curator.env.sample`
- `CURATOR_API_PORT` added to `curator.env.sample` (was read but undocumented)
- Exempt paths requiring no auth: `/login`, `/logout`, `/api/health`

## [0.19.2] - 2026-03-09

### Fixed
- **Tab navigation clears selection** — switching tabs resets `selectedIds` and closes any open kebab menu, preventing cross-tab bulk operations (e.g. selecting a pending + rejected torrent and triggering accept/queue)

### Docs
- Added Suggester engine acceptance criteria to `README.md` and `docs/PROJECT_SUMMARY.md` roadmap: exact-show rule vs franchise-broadening distinction, proactive suggestion before feed matches, confidence ranking with human-readable rationale
- Added scorer match-confidence signal to roadmap: `match_confidence` as a distinct scorer output field for detecting rule-vs-title semantic divergence (substring collisions, overly broad regexes, franchise spin-offs); response to low confidence kept as a product-layer decision

## [0.19.1] - 2026-03-09

### Fixed
- **Multi-select discipline** — per-card action buttons (accept/reject/queue) and the `⋮` kebab menu are hidden while ≥2 cards are selected; only bulk operations remain available in that state (`multiSelectActive` computed)
- **"Queue for dl" scoped to accepted torrents** — kebab menu only shows the queue option when `torrent.status === 'accepted'`; pending torrents must go through accept first, closing the feedback loop

## [0.19.0] - 2026-03-09

### Changed
- **Card interaction model** — cards are now click-to-select; checkboxes removed; selection indicated by filled `✓` badge + curator-green border + subtle bg tint
- Per-card action buttons (accept/reject on pending, queue on accepted) now appear only when the card is selected, keeping unselected cards clean
- `⋮` kebab button in every card header opens a dropdown with **⚡ re-score** and **⬇ queue for dl** — available regardless of selection state or active tab; click outside closes
- Bulk action bar: added **⬇ queue selected** button (accepted tab); added **✕** clear-selection button
- `toggleCard(id)` replaces `toggleSelection` as the unified card-click handler; also closes any open kebab menu on selection change

### Added
- `rescoreOne(id)` — single-card AI re-score from the kebab menu; calls `POST /api/torrents/rescore` with `{ids:[id]}`, merges updated score in-place, shows toast
- `openMenuId` reactive state tracks which card's kebab is open; `document` click listener closes menu on outside click

## [0.18.1] - 2026-03-09

### Fixed
- `CURATOR_AI_TIMEOUT_SECS` was being overridden by a hardcoded `http.Client.Timeout` on both the Ollama (60s) and OpenAI (30s) providers. `http.Client.Timeout` is a transport-level deadline that fires independently of the `context.WithTimeout` used per-request — so even with `CURATOR_AI_TIMEOUT_SECS=120`, the HTTP client cut the connection at 60s first. Both providers now use `&http.Client{}` (no transport timeout); the context deadline set by `CURATOR_AI_TIMEOUT_SECS` is now the sole authority.

## [0.18.0] - 2026-03-07

### Added
- **Per-subsystem model tiering** — each AI subsystem now resolves its model independently:
  - `CURATOR_AI_ENRICHER_MODEL` — fast/small model for regex fallback enrichment (e.g. `llama3.2:1b`)
  - `CURATOR_AI_SCORER_MODEL` — mid-size model for per-item history-aware scoring (e.g. `llama3.2`)
  - `CURATOR_AI_SUGGESTER_MODEL` — reserved for the suggestion engine; use the largest available model since it runs on-demand, not per-item (e.g. `llama3.1:8b`)
  - All three fall back to `CURATOR_AI_MODEL` if their subsystem-specific var is unset; `CURATOR_AI_MODEL` falls back to the provider's built-in default
- `NewProviderFor(subsystem string) Provider` — new constructor in `internal/ai/provider.go`; `NewProvider()` is now an alias for `NewProviderFor("")`
- Per-subsystem model vars documented with recommended examples in `curator.env.sample`

### Changed
- `cmdCheck` now constructs `enricherProvider` and `scorerProvider` separately via `NewProviderFor` instead of sharing a single provider
- `cmdServe` constructs `scorerProvider` via `NewProviderFor("scorer")` for the on-demand rescore endpoint

## [0.17.2] - 2026-03-07

### Fixed
- `scorer.response` log event emitted `score=<large integer>` (raw IEEE 754 bit-pattern) instead of the actual float value. Root cause: `logbuffer/zapcore.go` `fieldValue()` returned `f.Integer` directly for `Float64Type`/`Float32Type` fields; zap packs floats into `f.Integer` via `math.Float64bits()`, so the extractor now uses `math.Float64frombits(uint64(f.Integer))` / `math.Float32frombits(uint32(f.Integer))` to recover the real value.

### Changed
- `scoreSystemPrompt` scoring rules clarified: the `Match reason` field is now declared **authoritative** — the model is explicitly instructed not to re-evaluate whether the quality, codec, or group is appropriate (the deterministic matcher already did that). Technical signals are now scoped to differentiating between candidates that share the same match reason (e.g. prefer Atmos over non-Atmos when both match equally).

## [0.17.1] - 2026-03-07

### Fixed
- Hardcoded 15-second LLM timeout in `scoreOne` and `Enrich` caused `context deadline exceeded` errors when Ollama inference on large history prompts exceeded the limit. Both now default to 60 seconds.

### Added
- `CURATOR_AI_TIMEOUT_SECS` env var — configures the per-request inference timeout for both the scorer and enricher (default `60`). Documented in `curator.env.sample`.

## [0.17.0] - 2026-03-08

### Added
- **LLM I/O observability** — all AI interactions now emit structured DEBUG log events visible in the log drawer:
  - `scorer.request`: `torrent_id`, `title`, `user_prompt` logged before each `scoreOne` call
  - `scorer.response`: `score`, `reason`, `raw_response`, `duration_ms`, `error` logged after completion
  - `enricher.request`: `title`, `user_prompt` logged before each `Enrich` call
  - `enricher.response`: `show_name`, `season`, `episode`, `raw_response`, `duration_ms` logged after completion
- `Scorer.SetLogger(*zap.Logger)` — nil-safe method to attach a logger; wired in `NewServer` so scorer I/O surfaces in the server log drawer; CLI path (`cmdCheck`) passes `nil` (silent)
- `internal/ai/suggester.go` — `Suggester` struct with stable `Suggest(history, existing) ([]models.ShowRule, error)` interface; returns `ErrNotImplemented` until engine is built; doc comments describe intended TVDB/TMDb metadata workflow
- `POST /api/suggestions` — 501 stub with stable response shape `{"suggestions":[], "status":"not_implemented"}`; ready for engine implementation in a future release

### Changed
- `NewEnricher` signature updated to `NewEnricher(p Provider, logger *zap.Logger)` — logger field on `Enricher` enables I/O observability without breaking CLI callers (nil logger = no-op)
- `scoreSystemPrompt` rewritten with explicit **CONTENT SIGNALS (primary)** / **TECHNICAL SIGNALS (secondary)** framing; instructs model to weight content match above technical packaging preferences; includes explicit 0/50/100 score rule
- `scoreOne` user prompt restructured into two sections matching the system prompt: `Content signals` (Title, Show S##E##, Match reason) and `Technical signals` (Quality, Codec, Group, Source)
- `buildHistoryContext` — history lines now include `MatchReason` e.g. `[APPROVE] Show S01E03 (match: preferred_group:NTb)` so the model sees why each historical item was matched

## [0.16.0] - 2026-03-07

### Added
- `POST /api/torrents/rescore` — on-demand AI re-score for any set of torrents regardless of status; accepts `{"ids":[...]}`, returns `{"rescored":N, "torrents":[...]}` with fresh `ai_score`/`ai_reason`; returns `503` when AI provider is unreachable
- `Server` wired with `ai.Scorer` and `ai.Provider` at startup; `cmdServe` logs provider availability; scorer is now available during `serve` (not just `check`)
- UI: checkboxes on all three torrent tabs (pending, accepted, rejected) — previously only pending had them
- UI: `⚡ re-score` button in bulk action bar — always visible when items are selected across any tab; merges returned scores into the torrent list in-place without a full refresh; accept/reject bulk actions remain gated to the pending tab only
- `CURATOR_AI_HISTORY_SIZE` env var — configures the activity history window fed to the scoring prompt (default `40`, was hard-coded `20`)

### Changed
- `internal/ai.Scorer`: replaced naive tail-cut-20 history with stratified sampling (`sampleHistory`): approve and reject pools are deduplicated by title (most recent retained), balanced up to `size/2` each with overflow fill, then recombined sorted by `ActionAt` ascending so the model sees a temporal narrative — prevents a burst of one action type from dominating the prompt context as history grows

## [0.15.1] - 2026-03-07

### Added
- Stats mini-panel: bar chart icon + `stats` label heading above the tile grid (expanded state only)
- Log drawer: drag handle now functional — mousedown/mousemove/mouseup resizes drawer height dynamically, clamped between 80px and 92vh; CSS transition suppressed during active drag for zero-lag feel
- Log drawer: `↓ new / ↑ old` sort toggle button — defaults to newest-first; auto-scroll direction tracks sort order (top when descending, bottom when ascending)

## [0.15.0] - 2026-03-07

### Added
- **Stats mini-panel** — persistent 6-tile grid in the right sidebar (below dark mode toggle): Pending (live), Seen 24h, Staged 24h, Approved 24h, Rejected 24h, Queued 24h; collapsed sidebar shows pending count badge
- **Live log drawer** — DevTools-style bottom panel triggered from sidebar; slides up to 60vh; streams application logs in real-time over SSE (`GET /api/logs/stream`); backfills from ring buffer on open (`GET /api/logs`); INFO/WARN/ERROR level badge filters, text search, auto-scroll toggle, clear button
- `internal/logbuffer` package — thread-safe ring buffer (cap 500), `zapcore.Core` tee integration; all zap log entries captured in-memory for SSE delivery
- `GET /api/logs` — returns buffered log entries as JSON; accepts `?since=<id>` for incremental polling
- `GET /api/logs/stream` — SSE endpoint, fans out live entries to all connected browsers
- `storage.GetWindowStats(hours int)` — new `Store` interface method; 5 SQLite windowed COUNT queries on indexed timestamp columns

### Changed
- `GET /api/stats` — now returns 7 fields (Hours, Seen, Staged, Approved, Rejected, Queued, Pending) from `GetWindowStats(24)` instead of 3 all-time totals
- Header stats grid removed; stats moved to sidebar panel and sourced from API
- Go module path renamed `github.com/iillmaticc/rss-curator` → `github.com/killakam3084/rss-curator` (reconciles with public GitHub/GHCR identity)
- Compiled binary `curator` purged from git history and gitignore anchored to repo root (`/curator`)

## [0.14.2] - 2026-03-07

### Added
- `docs/ARCHITECTURE.md` — three Mermaid diagrams: system topology (`flowchart LR`), torrent state machine (`stateDiagram-v2`), and data model (`erDiagram`); includes state semantics table and package map
- `docs/` directory hierarchy — all reference docs moved out of root

### Changed
- `README.md` — architecture section replaced with condensed Mermaid diagram + link to `docs/ARCHITECTURE.md`; added AI configuration variables (`CURATOR_AI_PROVIDER`, `CURATOR_AI_HOST`, `CURATOR_AI_MODEL`, `CURATOR_AI_KEY`); added Web UI and `curator serve` to Usage; added missing features (Web UI, AI scoring, `shows.json`); updated project structure; fixed roadmap (removed shipped items `Web UI for approvals`, `Statistics and reporting`, `Duplicate detection`)
- `docs/PROJECT_SUMMARY.md` — full rewrite: v0.14.2, all 7 components documented (AI subsystem, API server, Web UI), complete CLI command table, updated project structure + dependencies, removed stale roadmap items
- `START_HERE.md` — updated doc table with `docs/` paths and new `docs/ARCHITECTURE.md` entry; updated features, project structure, stats, and all doc cross-references
- `.github/DEPLOYMENT.md` — updated Quick Links to `docs/` paths
- `cmd/curator/main.go` — `version` constant corrected to `0.14.2` (was stale at `0.5.1`)

### Removed
- `CONTAINER_IMPLEMENTATION.md` — post-implementation work notes, fully superseded by `docs/CONTAINER_GUIDE.md`
- `CONTAINER_SETUP.md` — same
- `CONTAINER_QUICKREF.md` — same
- `OVERVIEW.txt` — stale ASCII stats panel duplicating `docs/PROJECT_SUMMARY.md`

### Moved
- `PROJECT_SUMMARY.md` → `docs/PROJECT_SUMMARY.md`
- `QUICKSTART.md` → `docs/QUICKSTART.md`
- `CONTAINER_GUIDE.md` → `docs/CONTAINER_GUIDE.md`
- `TRUENAS_DEPLOYMENT.md` → `docs/TRUENAS_DEPLOYMENT.md`

## [0.14.1] - 2026-03-07

### Fixed
- `ai_scored` boolean column added to `staged_torrents` (idempotent `ALTER TABLE` migration); distinguishes "never scored" (`ai_scored=false`) from "scored with zero confidence" (`ai_scored=true, ai_score=0.0`)
- `UpdateAIScore()` now sets `ai_scored = 1` so every scoring attempt is recorded regardless of the resulting score value
- `Add()` INSERT and all `SELECT`/`Scan` call-sites updated to include `ai_scored`
- `AIScored bool` field added to `models.StagedTorrent` and `api.TorrentResponse`
- Backfill in `cmdCheck` now covers **all statuses** (not just `pending`) and filters on `ai_scored=false` instead of `ai_score==0`; always calls `UpdateAIScore` after scoring (even a 0.0 result marks the row as scored)
- UI score badge condition changed from `ai_score > 0` to `ai_scored` so that a genuinely low-confidence score (`⚡ 0%`) is shown rather than hidden
- UI sort guard (`hasScores`) updated to match the same `ai_scored` condition

## [0.14.0] - 2026-03-06

### Added
- `internal/ai` package with pluggable LLM provider abstraction (`Provider` interface, `OllamaProvider`, `OpenAIProvider`, `noopProvider`)
- `Enricher` — fallback metadata filler that calls the LLM when the regex parser leaves `ShowName` or `Season` empty; fully silent on provider errors
- `Scorer` — ranks staged torrents 0–1 against recent approve/reject history; no-ops when provider is unavailable
- `feed.Parser.WithEnricher()` — optional method to attach an `Enricher` to the parser pipeline
- AI scoring wired into `cmdCheck`: history fetched, `scorer.ScoreAll()` called after `MatchAll`, scores stored at insert time
- `ai_score` and `ai_reason` columns added to `staged_torrents` (idempotent `ALTER TABLE` migrations)
- `UpdateAIScore()` method on `Store` interface and `*Storage` implementation
- `AIScore` / `AIReason` fields on `models.StagedTorrent` and `api.TorrentResponse`
- UI: pending torrents sorted by `ai_score` descending when any scores are present
- UI: `⚡ N%` score badge on each torrent card (visible only when `ai_score > 0`), tooltip shows `ai_reason`
- AI env vars documented in `curator.env.sample` and `local.env.sample` (`CURATOR_AI_PROVIDER`, `CURATOR_AI_HOST`, `CURATOR_AI_MODEL`, `CURATOR_AI_KEY`)

### Fixed
- `handleApprove` now calls `LogActivity(action="approve")` so approvals are recorded as training signal alongside existing `queue` and `reject` events

## [0.13.7] - 2026-03-06

### Added
- `compose.dev.yml` — Podman-native local development stack with bridge networking, port mapping, and live web asset volume mounts (no rebuild needed for HTML/CSS/JS changes)
- `local.env.sample` — simplified local dev environment template with `host.containers.internal` hint for reaching host-side qBittorrent from inside a container
- `.gitignore` — excludes built binary, `local.env`, `.env`, `data/`, and `logs/`

### Changed
- Makefile rewritten to use Podman as default container runtime (`CTR ?= podman`, overridable with `CTR=docker`)
- Added `dev-up`, `dev-down`, `dev-logs`, `dev-rebuild`, `dev-clean` targets for a fast local feedback loop
- Renamed production image targets to `image-build`, `image-push`, `image-clean` for runtime-agnostic naming
- Removed Docker-specific `docker-build`, `docker-run`, `docker-push`, `docker-clean` targets

## [0.13.6] - 2026-03-05

### Fixed
- Improved dark-mode readability for rejected status badges by increasing text contrast against the red badge background

## [0.13.5] - 2026-03-05

### Changed
- Polished the light-mode empty state for the no-pending-torrents view with a warm gradient surface
- Added subtle depth via refined border and shadow treatment for stronger visual hierarchy
- Improved empty-state messaging with clearer primary and supporting text

## [0.13.4] - 2026-03-05

### Changed
- Softened light mode background from pure white to gentle blue-gray (rgb 247 250 252) for reduced eye strain
- Enhanced card contrast by using crisp white surfaces against the softer background
- Added subtle color accents throughout light mode (soft blue, blush pink, mint green badges)
- Improved geometric pattern with green tint to complement curator theme
- Strengthened border visibility for better component definition


## [0.13.3] - 2026-03-05

### Changed
- Refined light mode aesthetics with softer green accents (curator color palette adjusted from neon to forest green tones)
- Reduced geometric pattern opacity in light mode for subtler visual texture
- Improved light mode shadow effects for better depth perception without overwhelming contrast

## [0.13.2] - 2026-03-05

### Fixed
- Normalized whitespace formatting in matcher logic to satisfy CI `gofmt` checks

## [0.13.1] - 2026-03-05

### Added
- **Contribution Workflow Skill**: Added reusable Copilot skill defining contribution, testing, linting, formatting, changelog, tagging, and push flow
- Repository-level Copilot instructions to enforce the contribution workflow skill for commit/release tasks

### Changed
- CI orchestration now gates container build/push on passing `test` and `lint` phases via job dependencies
- Consolidated pipeline logic into a single workflow path to avoid duplicate test/lint executions

## [0.13.0] - 2026-03-05

### Added
- **Unit Testing Infrastructure**: Comprehensive test suite for API handlers and storage layer
- API handler tests (11 tests) covering approve, reject, queue, and state transition workflows
- Storage layer tests (5 tests) covering CRUD operations and activity logging
- Storage interface abstraction for better testability and dependency injection
- Test setup with mock storage for isolated API testing

### Changed
- Server struct now accepts `storage.Store` interface instead of concrete `*storage.Storage` for improved testability
- NewServer function signature updated to use storage.Store interface

### Fixed
- Fixed redundant newlines in fmt.Println calls for cleaner output

## [0.12.0] - 2026-03-05

### Added
- **Light Mode Support**: Full light/dark mode toggle with persistent user preference
- Light mode saves to localStorage; respects system preference if not saved
- Light mode color scheme with white background and dark text
- Tailwind class-based dark mode for conditional styling

### Changed
- Background pattern reduced opacity for better visibility in both light and dark modes
- Dark mode now uses localStorage to persist user preference across sessions

## [0.11.4] - 2026-03-05

### Fixed
- Rejection flow no longer triggers queue-for-download modal/actions
- `rejectTorrent()` now performs only reject behavior (status update + activity refresh)

### Changed
- Removed remaining `savePath` usage from bulk queue payloads and form state
- Queue configuration now consistently uses only `tags` and `category`

## [0.11.3] - 2026-03-05

### Fixed
- Fixed TypeError in submitReview when accessing ID after modal close
- Removed savePath field from configuration modal (qBittorrent web API limitation)
- Modal now only shows tags and category fields (working qBittorrent API options)

## [0.11.2] - 2026-03-05

### Changed
- **Accepted tab**: Replaced "retry qbittorrent" with "queue for dl" button
- **Modal UX**: "Queue for download" button now opens configuration modal for consistency
- `queueForDownload()` opens review modal instead of directly queuing
- Button styling changed from amber (retry) to curator-green (queue)
- Both "accept" and "queue for dl" paths now lead to modal for configuration

### Technical
- Renamed `retryQBittorrent()` to `queueForDownload()` in frontend
- Backend `handleQueue` now parses request body for savePath, tags, category
- Configuration options passed to qBittorrent AddTorrent call
- Fixed retry-qbittorrent endpoint to validate 'accepted' status instead of 'approved'

## [0.11.1] - 2026-03-05

### Fixed
- Removed undefined `approvedCount` and `reviewCount` references in Vue setup causing ReferenceError

## [0.11.0] - 2026-03-05

### Changed
- **UX Refactor**: Improved conceptual clarity of torrent workflow
- State transition renamed: `review` → `accepted` (better reflects user decision)
- Tab structure simplified: `pending → accepted → rejected` (removed `approved` tab)
- Action button labeled: `approve` → `accept` for clarity when transitioning from pending
- Modal title updated: `Review & Configure` → `Queue for Download` for clarity on next action

### Technical
- Backend `handleApprove` now sets status to `"accepted"`
- `handleQueue` validates torrents are in `"accepted"` status before queuing
- Cleaner state machine: accept decision is separate from queueing for download

## [0.10.2] - 2026-03-05

### Fixed
- Backend now correctly sets status to 'review' (instead of 'approved') when approve is called
- UI review modal now properly displays after approving a torrent
- Frontend fetches updated torrent object before opening review modal
- handleQueue validation now checks for 'review' status correctly

## [0.10.1] - 2026-03-05

### Fixed
- Frontend now correctly calls `/api/torrents/{id}/approve` instead of non-existent `/review` endpoint
- Resolved 400 Bad Request error when approving torrents from the UI

## [0.10.0] - 2026-03-05

### Added
- Backend support for two-step torrent queueing workflow
- New `/api/torrents/{id}/queue` endpoint for explicit queueing
- State validation: only approved torrents can be queued

### Changed
- **Breaking**: `approve` action no longer automatically queues to qBittorrent
- Approve now only marks torrents as approved (tollgate entry)
- Queue action must be called separately to send torrents to qBittorrent
- Improved error handling for qBittorrent client availability

### Technical
- New `handleQueue()` function for explicit torrent queueing
- Queue validates torrent status before attempting to add to qBittorrent
- Better separation of concerns: approval decision vs. download execution

## [0.9.0] - 2026-03-05

### Added
- **Tollgate Review State**: Approval now leads to review/config modal, not direct download
- Individual review modal for per-torrent configuration (savePath, tags, category)
- Bulk review modal for configuring multiple torrents at once
- `deferReview()` to skip config and queue later
- `bulkQueue()` to queue multiple torrents with defaults (quick batch mode)
- `submitBulkReview()` to queue multiple torrents with custom config (configured batch mode)

### Changed
- **Semantic state transitions**: pending → approved (tollgate) → queued/downloading
- Approval no longer directly triggers download; review/config is mandatory by default
- `submitReview()` now calls `/api/torrents/{id}/queue` endpoint
- Layout precision: console and main content stay tightly aligned with px-based widths
- Enhanced bulk operations: users can choose between quick or configured batch queuing

### Technical
- New state: `bulkReviewModalOpen`, `bulkReviewForm` for bulk configuration
- Three queue endpoints: individual review → queue, quick bulk → queue, configured bulk → queue
- Decoupled approval from download queueing for better UX control

### UX Improvements
- Users can approve without immediate config burden (defer)
- Batch operations support both speed (quick queue) and control (configured queue)
- Flexible approval workflow: immediate config OR defer for later batch processing

## [0.8.2] - 2026-03-05

### Added
- Idempotent schema migration system for safe database upgrades
- Raw feed item persistence in `curator check` command
- Debug logging for feed stream endpoint to track retrieval count
- 24-hour TTL for raw feed items with automatic expiration

### Fixed
- Database migration now creates `raw_feed_items` table on existing databases
- Feed stream endpoint now properly displays discovered items (not just matched ones)
- Migration errors no longer cause startup failures (graceful handling)

### Technical
- Separate migration strategy with idempotent SQL statements
- Background cleanup of expired raw feed items on every feed stream request
- All discovered torrents from RSS feeds are now persisted for console visibility

## [0.8.1] - 2026-03-04

### Added
- `RawFeedItem` model for tracking unfiltered RSS feed pulls
- `raw_feed_items` table with TTL-based cleanup for temporary feed visibility
- Storage methods: `AddRawFeedItem()`, `GetRawFeedItems()`, `CleanupExpiredRawFeedItems()`
- Backend support for tracking all torrents pulled from RSS (not just matched ones)

### Changed
- Renamed sidebar to "console" throughout UI for clarity
- `/api/feed/stream` now returns raw feed items instead of matched torrents
- Console layout: left column content stays visible and fits-to-width when console expands/collapses
- Feed stream shows all discovered items with "discovered" status vs. matched/approved/rejected
- Fixed responsive behavior when toggling console collapse state

### Fixed
- Main content area now properly constrains width based on console state
- Layout transitions smoothly without content jumping

## [0.8.0] - 2026-03-04

### Added
- Full-height fixed navigation sidebar (300px wide, right-side)
- Hamburger toggle for collapsing sidebar to icon-only strip (64px)
- Dark mode toggle integrated into sidebar (visible in both states)
- New backend endpoint `/api/feed/stream` for real-time RSS feed discoveries
- Jenga-style slide/fade-in animations for feed stream items
- Live feed ticker pulling from actual RSS feed data (refreshes every 30s)
- Support for future admin features (settings panel placeholder)

### Changed
- Sidebar refactored from relative grid position to fixed full-height panel
- Feed stream now uses real backend data instead of static torrent list
- Layout changed from grid to flex to accommodate fixed sidebar
- Dark mode toggle moved from header to sidebar
- Main content area now uses dynamic margin based on sidebar state
- Sidebar collapse behavior: hamburger ☰ when collapsed, ✕ when expanded

### Technical
- Added `FeedStreamItem` and `FeedStreamResponse` types to server
- Implemented `handleFeedStream()` handler with sorting and timestamp simulation
- Frontend `fetchFeedStream()` function integrated with 30s polling
- CSS animations for smooth slide-in effects from top
- Proper z-index layering for fixed sidebar

## [0.7.4] - 2026-03-03

### Added
- Collapsible sidebar with toggle button (defaults open)
- Tab switcher between Activity Log and Feed Stream
- Feed Stream ticker showing all discovered torrents
- Vertical scrolling ticker display with title, size, and match reason
- Visual highlighting for matched/approved torrents in lime
- Muted styling for pending torrents
- Sidebar collapse state persistence via localStorage

### Changed
- Reorganized sidebar to support multiple content panels
- Main content now expands to full width when sidebar is collapsed

## [0.7.3] - 2026-03-03

### Changed
- Complete UI redesign with hacker aesthetic
- Shift from gradient to dark charcoal background (#1a1a1a)
- Replace purple/blue palette with electric lime (#00ff41) accents
- Implement angular geometric triangular pattern background
- Convert typography to bold monospace (Monaco/Courier)
- Terminal-style UI with borders, glow effects, and uppercase commands
- All elements now use dark theme with lime highlights and hover states
- Enhanced retro-hacker vibe throughout interface

## [0.7.2] - 2026-03-03

### Fixed
- Dark mode initialization to synchronously read system preference
- Dark mode toggle button now works correctly in both directions
- Vue warnings about undefined darkMode and toggleDarkMode properties during render

## [0.7.1] - 2026-03-03

### Added
- Dark mode toggle with system preference detection
- Automatic theme switching as system preference changes throughout the day
- Manual override capability via toggle button in header

### Changed
- Dark mode now defaults to system preference instead of localStorage
- Listens for system color scheme changes and updates in real-time

## [0.7.0] - 2026-03-03

### Changed
- Complete UI redesign with Tailwind CSS for modern, sleek interface
- Responsive grid layout for torrent cards (1 col mobile, 2 col tablet+)
- Enhanced visual hierarchy with improved typography and spacing
- Better status badges with color-coded states (blue/emerald/red)

### Added
- Smooth animations and transitions throughout the UI
- Custom scrollbar styling in activity log
- Gradient headers and stat cards with hover effects
- Dark mode support (ready for toggle implementation)
- Improved button states and focus indicators
- Better empty state and loading state visuals

## [0.6.10] - 2026-02-26

### Added
- Comprehensive configuration logging at startup showing all environment variables and parsed values
- Debug logging in AddTorrent() to track paused state through the qBittorrent client
- Full visibility into final options map before sending to qBittorrent API

## [0.6.9] - 2026-02-26

### Added
- Comprehensive configuration logging at startup showing all environment variables and parsed values
- Debug logging in AddTorrent() to track paused state through the qBittorrent client
- Full visibility into final options map before sending to qBittorrent API

### Changed
- Default behavior: torrents now added to qBittorrent in paused state
- Prevents accidental downloads during testing and iteration
- Can override with `QBITTORRENT_ADD_PAUSED=false` for production auto-start

## [0.6.8] - 2026-02-26

### Added
- `cleanup` command to remove stale database entries with pattern matching
- Feed parser logging showing whether parsed URLs are authenticated downloads or info pages
- `CleanupStaleLinks()` storage method for removing entries by URL pattern
- Default cleanup removes pending entries with IPTorrents info page links (`/t/{id}`)

### Fixed
- Observability for tracking parsed link formats (helps diagnose database staleness)
- Tooling to manage database when RSS feed format or URL structure changes

## [0.6.7] - 2026-02-26

### Changed
- Removed dead URL transformation logic that couldn't work without authentication cookies
- RSS feeds now used as-is with pre-authenticated URLs containing `torrent_pass` tokens
- Simplified AddTorrent and RetryAddTorrent to trust RSS feed URLs directly

### Removed
- `transformTorrentURL()` function and related conditional transformation logic
- IPTorrents info page (`/t/{id}`) pattern detection (requires auth that info pages cannot provide)

## [0.6.6] - 2026-02-26

### Fixed
- Preserve authenticated query parameters in torrent URLs from RSS feeds
- RSS feeds provide fully authenticated URLs with `torrent_pass` that must not be stripped
- Skip URL transformation when URLs contain query parameters (detected by `?`)
- URLs without query parameters still attempted transformation for info page links (`/t/{id}`)

### Added
- Enhanced debug logging for bencoded errors explaining common causes
- Detailed error messages indicating missing authentication cookies as likely cause

## [0.6.5] - 2026-02-27

### Fixed
- RetryAddTorrent now applies URL transformation to info page links
- Retry operations now correctly convert IPTorrents `/t/{id}` to `/download.php/{id}/{title}.torrent`
- Torrents now actually appear in qBittorrent when using manual retry

## [0.6.4] - 2026-02-27

### Added
- Manual retry capability for qBittorrent torrent addition
- POST `/api/torrents/{id}/retry-qb` endpoint for manual retries
- `RetryAddTorrent()` method with exponential backoff (3 attempts, 500ms-5s delays)
- "Retry qBittorrent" button in approved torrents tab UI
- Detailed logging of retry attempts and results

### Changed
- AddTorrent now performs single non-blocking attempt (no automatic retries)
- Approval and qBittorrent integration are now completely decoupled
- Users can manually retry failed qBittorrent additions without re-approving

## [0.6.3] - 2026-02-27

### Added
- URL transformation logic for torrent info page links
- IPTorrents pattern detection and conversion (`/t/{id}` → `/download.php/{id}/{title}.torrent`)
- Support for URL encoding titles with spaces and special characters
- Title parameter passing from API handler to qBittorrent client

### Changed
- transformTorrentURL() now accepts title parameter for proper filename construction
- AddTorrent extracts title from options map before calling transformation

## [0.6.2] - 2026-02-27

### Added
- Enhanced connection logging to qBittorrent client showing host, username, and test results
- Detailed logging in AddTorrent with URL scheme detection
- Torrent count verification after successful qBittorrent addition

### Changed
- Approval workflow decoupled from qBittorrent integration
- Status update happens immediately regardless of qBittorrent availability
- qBittorrent add moved to non-blocking async goroutine
- Activity logged before qBittorrent attempt for audit trail accuracy

## [0.6.1] - 2026-02-26

### Fixed
- Header stats (Pending/Approved/Rejected) now update correctly after operations
- Bulk operations now refresh all torrent statuses instead of just active tab
- Auto-refresh now fetches all statuses for accurate count synchronization

### Added
- GET `/api/stats` endpoint returning approved/rejected counts from activity log
- Historical stats sourced from audit trail for accurate reporting

### Changed
- Frontend `fetchAllTorrents()` method fetches and merges all three statuses in parallel
- All approve/reject/bulk operations use unified data fetch strategy
- Approved/Rejected counts now sourced from activity_log for audit trail accuracy

## [0.6.0] - 2026-02-26

### Added
- Activity log system with SQLite persistence for audit trail
- Activity struct data model with ID, TorrentID, TorrentTitle, Action, ActionAt, MatchReason
- `activity_log` SQLite table with proper schema and indexes
- Storage layer methods: LogActivity(), GetActivity(), GetActivityCount()
- GET `/api/activity` endpoint with limit/offset pagination and action filtering
- Activity sidebar UI component displaying recent activities
- Color-coded action badges (green for approve, red for reject)
- Automatic logging on torrent approve/reject operations
- Responsive layout with 2-column grid (main content + sidebar)
- Activity display includes torrent title, match reason, and timestamp
- Auto-refresh of activity log every 30 seconds alongside torrents

### Improved
- UI layout now features activity log sidebar separate from main torrent list
- Better visual organization with negative space and padding
- Bootstrap-ish button styling with proper active/disabled states
- Cleaner torrent card presentation in organized grid

## [0.5.1] - 2026-02-26

### Added
- Toast notifications for success/error feedback
- Loading spinners on buttons during operations
- Per-torrent operation loading state

### Improved
- Auto-clear selections after successful bulk operations
- Disabled buttons while requests in flight
- Better visual feedback during operations
- Bulk operation counter showing success rate

## [0.5.0] - 2026-02-26

### Added
- Vue 3 framework integration via CDN for interactive frontend
- Multi-select checkboxes for bulk torrent operations
- Bulk approve/reject functionality for selected torrents
- Tab-based filtering (pending/approved/rejected)
- Reactive state management with computed properties
- Auto-refresh every 30 seconds
- Better UI responsiveness with Vue reactivity
- Selection counter showing selected torrent count

### Changed
- Dashboard now uses Vue 3 for improved interactivity
- Refactored frontend JavaScript to Vue 3 Composition API
- Better status filtering with tab navigation
- Enhanced torrent selection and batch operations

## [0.4.2] - 2026-02-26

### Added
- Zap structured logging framework for production-grade logging
- Comprehensive logging throughout API handlers with appropriate log levels
- Structured fields for all log entries (IDs, titles, statuses, errors)
- Better error context and debugging information

### Changed
- Replace fmt.Printf debug logging with zap logger
- Info logs for successful operations
- Warn logs for validation failures
- Error logs for internal errors

## [0.4.1] - 2026-02-26

### Fixed
- API route path parsing for torrent actions - correctly trim /api/torrents/ prefix
- Missing closing brace in handleTorrentAction switch statement
- Add comprehensive logging to diagnose 400 errors and data persistence issues
- Torrent ID parsing and database lookup validation

## [0.4.0] - 2026-02-26

### Added
- Web UI dashboard for torrent review and approval workflow
- HTML/CSS/JavaScript dashboard interface serving on `/`
- Auto-refresh capability (30-second interval toggle)
- Real-time torrent list display with approve/reject buttons
- Stats cards showing pending/approved/rejected torrent counts
- Manual refresh button for immediate data updates
- Responsive design supporting mobile and desktop viewports
- Static file serving from `/style.css` and `/app.js`

### Changed
- API endpoints now prefixed with `/api/` (e.g., `/api/torrents`, `/api/torrents/{id}/approve`)
- API server now serves complete web dashboard in addition to REST endpoints
- Root endpoint (`/`) now serves dashboard HTML instead of API info

## [0.3.1] - 2026-02-25

### Fixed
- Graceful handling of qBittorrent unavailability - API server now starts even if qBittorrent is unreachable
- Container no longer crashes if qBittorrent is not available on startup

## [0.3.0] - 2026-02-25

### Added
- REST HTTP API for torrent review operations
- GET `/api/torrents` - list torrents by status
- POST `/api/torrents/{id}/approve` - approve a torrent and add to qBittorrent
- POST `/api/torrents/{id}/reject` - reject a torrent
- GET `/api/health` - health check endpoint
- Dual-mode execution: scheduler running in background + API server in foreground
- `start.sh` orchestration script for dual-mode operation

### Changed
- Application now runs both scheduler and API server simultaneously
- CURATOR_API_PORT environment variable controls API server port (default: 8081)

## [0.2.0] - 2026-02-25

### Added
- Internal scheduler for periodic RSS feed checks (configurable via `CHECK_INTERVAL` env var)
- `scheduler.sh` script that runs checks on a fixed interval
- Scheduler baked into Docker image as default entrypoint
- Support for `shows.json` configuration file for dynamic show list management
- Logging infrastructure with output to `/app/logs/curator.log`
- GitHub Actions workflow for automated GHCR publishing
- Comprehensive Docker deployment documentation

### Changed
- Replaced cron-based scheduling with internal container scheduler
- Simplified docker-compose configuration
- Container now stays running continuously instead of restarting after each check

### Fixed
- CGO build failures by adding gcc, musl-dev, and sqlite-dev to Dockerfile
- GHCR image accessibility issues

## [0.1.0] - 2026-02-20

### Added
- Initial Docker containerization with multi-stage build
- GitHub Container Registry (GHCR) publishing
- TrueNAS Docker Compose support
- qBittorrent integration for torrent management
- RSS feed parsing and show matching
- SQLite database for tracking matched items
- Environment variable configuration support
- Container documentation and deployment guides
