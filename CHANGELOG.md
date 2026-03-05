# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
