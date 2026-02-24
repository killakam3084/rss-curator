# RSS Curator - Quick Start Guide

Get up and running in 5 minutes.

## Prerequisites

- Go 1.22+
- qBittorrent with Web UI enabled
- RSS feed URL from your private tracker

## 1. Build

```bash
cd rss-curator
make build
```

## 2. Configure

Copy the sample config:

```bash
cp curator.env.sample ~/.curator.env
```

Edit `~/.curator.env` with your settings:

```bash
vim ~/.curator.env
```

**Minimum required settings:**

```bash
export RSS_FEED_URL="https://your-tracker.com/rss?passkey=YOUR_PASSKEY"
export QBITTORRENT_USER="admin"
export QBITTORRENT_PASS="your-password"
export SHOW_NAMES="Show Name 1,Show Name 2"
```

## 3. Test

Load config and test connections:

```bash
source ~/.curator.env
./curator test
```

You should see:

```
Testing connections...
qBittorrent... ✓ Connected
  Active torrents: 5
RSS feed 1... ✓ OK (47 items)
```

## 4. Check for Matches

Scan your RSS feed:

```bash
./curator check
```

## 5. Review and Approve

List pending torrents:

```bash
./curator list
```

Approve specific torrents:

```bash
./curator approve 1 3 5
```

Or use interactive mode:

```bash
./curator review
```

## 6. Automate (Optional)

### Option A: Cron

Add to crontab:

```bash
*/30 * * * * source ~/.curator.env && /path/to/curator check
```

### Option B: Systemd Timer

See the full README for systemd setup instructions.

## Common Commands

```bash
curator check              # Check feeds and stage matches
curator list               # List pending torrents
curator list approved      # List approved torrents
curator approve 1 2 3      # Approve specific torrents by ID
curator reject 4 5         # Reject specific torrents
curator review             # Interactive review mode
curator test               # Test configuration
```

## Tips

1. **Start conservative**: Set `MIN_QUALITY="1080p"` initially
2. **Use preferred groups**: Private trackers usually have trusted release groups
3. **Check regularly**: Run `curator check` every 30 minutes via cron
4. **Review first**: Use `curator review` mode for your first few runs
5. **Clean up old entries**: The database keeps history - approved/rejected items are retained

## Troubleshooting

### Can't connect to qBittorrent

1. Check Web UI is enabled in qBittorrent settings
2. Verify the host/port in your config
3. Test manually: `curl http://localhost:8080/api/v2/app/version`

### No matches found

1. Check your `SHOW_NAMES` spelling
2. Try with no `SHOW_NAMES` to match everything (temporarily)
3. Look at the RSS feed title format
4. The matching is substring-based and case-insensitive

### RSS feed returns 403

Your RSS feed URL may have expired. Generate a new one from your tracker.

## Next Steps

- Read the full [README.md](README.md) for detailed documentation
- Set up automation with cron or systemd
- Customize your matching rules
- Consider adding preferred release groups

Happy curating! 🎬
