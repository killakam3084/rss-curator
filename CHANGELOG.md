# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
