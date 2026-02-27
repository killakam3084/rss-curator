# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
