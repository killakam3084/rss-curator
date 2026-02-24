# 🎬 RSS Curator

A semi-automated torrent curator for private tracker RSS feeds with human-in-the-loop approval.

## Features

- ✅ Parse RSS feeds from private trackers
- ✅ Intelligent metadata extraction (show name, season, episode, quality, codec, release group)
- ✅ Rule-based matching (quality filters, preferred codecs, release group preferences)
- ✅ SQLite-based staging system
- ✅ Integration with qBittorrent Web API
- ✅ Interactive CLI for approvals
- ✅ Batch operations support

## Installation

### Prerequisites

- Go 1.22 or later
- qBittorrent with Web UI enabled
- SQLite3

### Build from source

```bash
git clone https://github.com/iillmaticc/rss-curator
cd rss-curator
go build -o curator ./cmd/curator
```

### Install

```bash
# Copy to your PATH
sudo cp curator /usr/local/bin/
```

## Configuration

RSS Curator uses environment variables for configuration:

### Required Variables

```bash
export RSS_FEED_URL="https://your-tracker.com/rss?passkey=..."
export QBITTORRENT_USER="admin"
export QBITTORRENT_PASS="your-password"
```

### Optional Variables

```bash
# qBittorrent settings
export QBITTORRENT_HOST="http://localhost:8080"      # Default
export QBITTORRENT_CATEGORY="curator"                # Default
export QBITTORRENT_SAVEPATH="/path/to/downloads"     # Default: qBittorrent default

# Matching rules
export SHOW_NAMES="The Expanse,Foundation,Severance"
export MIN_QUALITY="1080p"                            # 720p, 1080p, 2160p
export PREFERRED_CODEC="x265"                         # x264, x265
export EXCLUDE_GROUPS="YIFY,RARBG"                    # Comma-separated
export PREFERRED_GROUPS="NTb,FLUX,HMAX"               # Comma-separated

# Storage
export STORAGE_PATH="$HOME/.curator.db"               # Default
```

### Create a config script

For convenience, create a `~/.curator.env` file:

```bash
#!/bin/bash
# RSS Curator Configuration

# Tracker RSS feed
export RSS_FEED_URL="https://your-tracker.com/rss?passkey=YOUR_PASSKEY"

# qBittorrent
export QBITTORRENT_HOST="http://localhost:8080"
export QBITTORRENT_USER="admin"
export QBITTORRENT_PASS="your-password"
export QBITTORRENT_CATEGORY="tv-shows"

# Shows to watch
export SHOW_NAMES="The Last of Us,House of the Dragon,Severance,Foundation"

# Quality preferences
export MIN_QUALITY="1080p"
export PREFERRED_CODEC="x265"

# Release groups
export PREFERRED_GROUPS="NTb,FLUX,HMAX,CMRG"
export EXCLUDE_GROUPS="YIFY"
```

Then source it before running:

```bash
source ~/.curator.env
curator check
```

## Usage

### Test Configuration

```bash
curator test
```

Output:
```
Testing connections...
qBittorrent... ✓ Connected
  Active torrents: 5
RSS feed 1... ✓ OK (47 items)
```

### Check for New Items

Scan RSS feeds and stage matches:

```bash
curator check
```

Output:
```
Checking RSS feeds...
Fetching: https://your-tracker.com/rss?passkey=...
Found 47 items
Matched 3 items

✓ Staged 3 new torrents
Run 'curator list' to review pending items
```

### List Staged Torrents

```bash
curator list              # Show pending (default)
curator list pending      # Show pending
curator list approved     # Show approved
curator list rejected     # Show rejected
```

Output:
```
ID  TITLE                                                   SIZE      REASON                          DATE
--  -----                                                   ----      ------                          ----
15  The.Last.of.Us.S02E03.1080p.WEB-DL.x265-NTb            2.1 GB    matches show: The Last of Us    Jan 24 14:32
14  Foundation.S03E08.2160p.WEB-DL.x265-FLUX               4.8 GB    matches show: Foundation        Jan 24 12:15
13  Severance.S02E01.1080p.WEB-DL.x264-HMAX                3.2 GB    matches show: Severance         Jan 23 22:10
```

### Approve Torrents

Approve specific torrents by ID:

```bash
curator approve 15 14
```

Output:
```
Adding: The.Last.of.Us.S02E03.1080p.WEB-DL.x265-NTb
✓ Approved torrent 15
Adding: Foundation.S03E08.2160p.WEB-DL.x265-FLUX
✓ Approved torrent 14
```

### Reject Torrents

```bash
curator reject 13
```

### Interactive Review Mode

Review all pending torrents interactively:

```bash
curator review
```

Output:
```
[1/3] The.Last.of.Us.S02E03.1080p.WEB-DL.x265-NTb
      Size: 2.1 GB | Match: matches show: The Last of Us, quality: 1080p, preferred codec: x265
      Link: https://tracker.com/download/12345.torrent
      (a)pprove / (r)eject / (s)kip: a
✓ Approved

[2/3] Foundation.S03E08.2160p.WEB-DL.x265-FLUX
      Size: 4.8 GB | Match: matches show: Foundation, quality: 2160p, preferred group: FLUX
      Link: https://tracker.com/download/12346.torrent
      (a)pprove / (r)eject / (s)kip: s
Skipped

...

Review complete!
```

## Automation

### Cron Job

Add to your crontab to check feeds every 30 minutes:

```bash
crontab -e
```

Add:
```bash
*/30 * * * * source ~/.curator.env && /usr/local/bin/curator check >> ~/.curator.log 2>&1
```

### Systemd Timer (Recommended)

Create `/etc/systemd/system/curator.service`:

```ini
[Unit]
Description=RSS Curator Check
After=network.target

[Service]
Type=oneshot
User=your-username
EnvironmentFile=/home/your-username/.curator.env
ExecStart=/usr/local/bin/curator check
```

Create `/etc/systemd/system/curator.timer`:

```ini
[Unit]
Description=RSS Curator Timer
Requires=curator.service

[Timer]
OnBootSec=5min
OnUnitActiveSec=30min

[Install]
WantedBy=timers.target
```

Enable and start:

```bash
sudo systemctl enable curator.timer
sudo systemctl start curator.timer
```

Check status:

```bash
systemctl status curator.timer
journalctl -u curator.service
```

## Architecture

```
┌─────────────┐
│  RSS Feeds  │
└──────┬──────┘
       │
       ▼
┌─────────────┐      ┌──────────────┐
│   Parser    │─────▶│   Matcher    │
└─────────────┘      └──────┬───────┘
                            │
                            ▼
                     ┌──────────────┐
                     │   Storage    │
                     │  (SQLite)    │
                     └──────┬───────┘
                            │
                    ┌───────┴────────┐
                    │                │
                    ▼                ▼
              ┌──────────┐     ┌──────────┐
              │   CLI    │     │  qBit    │
              │ Commands │     │  Client  │
              └──────────┘     └──────────┘
```

### Components

- **Parser**: Fetches and parses RSS feeds, extracts metadata
- **Matcher**: Applies rules to filter items
- **Storage**: SQLite database for staging torrents
- **CLI**: User interface for management
- **qBittorrent Client**: Wrapper around qBittorrent Web API

## Development

### Project Structure

```
rss-curator/
├── cmd/
│   └── curator/
│       └── main.go           # CLI application
├── internal/
│   ├── feed/
│   │   └── parser.go         # RSS parsing
│   ├── matcher/
│   │   └── matcher.go        # Rule matching
│   ├── storage/
│   │   └── storage.go        # SQLite storage
│   └── client/
│       └── qbittorrent.go    # qBittorrent API client
├── pkg/
│   └── models/
│       └── types.go          # Shared types
├── go.mod
└── README.md
```

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o curator ./cmd/curator
```

### Cross-compilation

```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o curator-linux-amd64 ./cmd/curator

# macOS
GOOS=darwin GOARCH=arm64 go build -o curator-darwin-arm64 ./cmd/curator
```

## Roadmap

- [ ] YAML configuration file support
- [ ] Web UI for approvals
- [ ] Webhook notifications
- [ ] Season pack handling
- [ ] Duplicate detection
- [ ] Custom metadata extraction patterns
- [ ] Multi-tracker support
- [ ] Statistics and reporting

## Troubleshooting

### qBittorrent Connection Failed

Ensure Web UI is enabled:
1. Open qBittorrent
2. Tools → Options → Web UI
3. Enable "Web User Interface"
4. Set username/password
5. Note the port (default: 8080)

### RSS Feed Returns 403

Your RSS feed URL may have expired. Generate a new one from your tracker's settings.

### No Items Matched

Check your matching rules:
```bash
# Debug mode (coming soon)
curator check --debug
```

Verify your `SHOW_NAMES` includes the shows you want.

## License

MIT

## Contributing

Contributions welcome! Please open an issue or PR.

## Acknowledgments

- [autobrr/go-qbittorrent](https://github.com/autobrr/go-qbittorrent) - qBittorrent API client
- [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) - SQLite driver

---

Built with ❤️ for private tracker enthusiasts
