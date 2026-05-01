# 🎬 RSS Curator - START HERE

**A smart torrent curator for private tracker RSS feeds**

---

## 📦 What You Have

A complete, production-ready Go application for semi-automated torrent management.

```
rss-curator/
├── 📖 Documentation (docs/ + root)
├── 💻 Source Code (7 packages)
├── 🌐 Web UI (Vue.js dashboard)
├── 🤖 AI Subsystem (Ollama / OpenAI)
├── 🔧 Build Tools (Makefile, Docker)
└── ⚙️  Configuration (samples & templates)
```

---

## 🚀 Quick Start (Choose Your Path)

### Path 1: Local Development
```bash
cd rss-curator/
make build
cp curator.env.sample ~/.curator.env
vim ~/.curator.env  # Configure
source ~/.curator.env
./curator test
./curator check
```

### Path 2: TrueNAS Deployment
```bash
# See docs/TRUENAS_DEPLOYMENT.md
scp -r rss-curator user@truenas:/mnt/cell_block_d/apps/
# Then follow Docker Compose setup
```

### Path 3: Just Read First
```bash
# Start with README.md — installation, configuration, and usage all in one place
```

---

## 📚 Documentation Guide

| Document | Purpose | Read When |
|----------|---------|-----------|
| **README.md** | Complete reference guide — install, configure, use | Start here |
| **docs/ARCHITECTURE.md** | System topology, state machine, ER diagrams | Understanding internals |
| **docs/PROJECT_SUMMARY.md** | Technical deep-dive, all components | Understanding internals |
| **docs/CONTAINER_GUIDE.md** | Docker/Compose/GHCR reference | Container deployment |
| **docs/TRUENAS_DEPLOYMENT.md** | Deploy on TrueNAS SCALE | Production deployment |

---

## 🏗️ What's Included

### Documentation
- ✅ Quick start guide
- ✅ Comprehensive README
- ✅ Architecture diagrams (Mermaid — topology, state machine, ER)
- ✅ TrueNAS deployment guide
- ✅ Container guide

### Source Code
- ✅ RSS feed parser with metadata extraction
- ✅ Rule-based matcher engine
- ✅ SQLite storage layer with idempotent migrations
- ✅ qBittorrent API client
- ✅ HTTP API server (REST)
- ✅ Full-featured CLI application

### AI Subsystem
- ✅ Pluggable LLM provider (Ollama · OpenAI · disabled)
- ✅ Metadata enricher (fills ShowName/Season when regex fails)
- ✅ Approval probability scorer (0–100%) against approve/reject history

### Web UI
- ✅ Vue.js dashboard served at `http://localhost:8081`
- ✅ AI score badges, dark/light mode, activity log, feed stream

### Configuration
- ✅ Sample environment file
- ✅ Docker Compose setup
- ✅ Systemd service files
- ✅ Makefile for building

---

## ⚡ Features

- **Smart Parsing**: Extracts show name, season, episode, quality, codec, release group
- **Rule-Based Filtering**: Match by show names, quality, codec, release groups; `watchlist.json` for per-show config
- **AI Scoring**: Optional LLM-based approval probability (Ollama/OpenAI), learns from approve/reject history
- **Human-in-the-Loop**: Review and approve via CLI or Web UI before downloading
- **qBittorrent Integration**: Seamless torrent addition on approval
- **SQLite Storage**: Persistent staging with status tracking and activity log
- **Web Dashboard**: `curator serve` → `http://localhost:8081` for browser-based management
- **Automation Ready**: Cron or systemd timer support

---

## 🎯 Use Cases

### Personal Use (Recommended Start)
```bash
# Check feeds manually
curator check
curator review  # Approve interactively
```

### Semi-Automated
```bash
# Cron job checks every 30 min
# You review & approve daily
curator list    # See what's staged
curator approve 1 3 5
```

### Fully Automated (Future)
```bash
# Add auto-approval rules
# Cron checks and auto-approves matches
# You just monitor
```

---

## 🔧 Configuration

### Minimal Config (to get started)
```bash
export RSS_FEED_URL="https://tracker.com/rss?passkey=..."
export QBITTORRENT_USER="admin"
export QBITTORRENT_PASS="password"
export SHOW_NAMES="Show 1,Show 2"
```

### Full Config (for tuning)
```bash
# See curator.env.sample for all options
# Includes quality filters, codec preferences,
# release group management, etc.
```

---

## 🏃 Common Workflows

### Daily Check-In
```bash
curator check                 # Scan feeds
curator list                  # Review matches
curator review                # Approve interactively
```

### Batch Approval
```bash
curator list                  # See pending
curator approve 1 2 3 5 7     # Approve specific IDs
```

### Testing
```bash
curator test                  # Test connections
curator check                 # Dry run (stages only)
curator list pending          # See what would match
```

---

## 🐳 Deployment Options

### Option A: Docker Compose (Recommended for TrueNAS)
- Consistent with your existing setup
- Easy updates
- Network isolation via Gluetun
- See: `TRUENAS_DEPLOYMENT.md`

### Option B: Native Binary
- Direct execution
- Systemd integration
- Lower overhead
- See: `README.md` → Installation

### Option C: Cron Job
- Simplest automation
- Good for personal use
- See: `README.md` → Automation

---

## 📂 Project Structure

```
rss-curator/
├── cmd/curator/main.go              # CLI entry point, all command dispatch
├── internal/
│   ├── ai/                          # Provider interface, Enricher, Scorer
│   ├── api/                         # HTTP API server + Web UI handler
│   ├── client/qbittorrent.go        # qBit integration
│   ├── feed/parser.go               # RSS parsing + metadata extraction
│   ├── matcher/matcher.go           # Rule matching
│   └── storage/storage.go           # SQLite ops + migrations
├── pkg/models/types.go              # Data structures
├── web/                             # Vue.js dashboard
├── docs/                            # Reference documentation
│   ├── ARCHITECTURE.md             # Mermaid diagrams
│   ├── PROJECT_SUMMARY.md
│   ├── CONTAINER_GUIDE.md
│   └── TRUENAS_DEPLOYMENT.md
├── README.md
├── CHANGELOG.md
└── START_HERE.md
```

---

## 🎓 Learning Path

### Just Want It Working?
1. Read `README.md` — Installation + Configuration + Usage
2. Copy `curator.env.sample` to `~/.curator.env`
3. Configure your settings
4. Run `make build && ./curator test`
5. Start using it!

### Want to Understand It?
1. Read `docs/ARCHITECTURE.md` for diagrams
2. Read `docs/PROJECT_SUMMARY.md` for component deep-dive
3. Browse source code in `internal/` and `cmd/`
4. Check out the matching logic in `matcher.go`
5. Look at CLI commands in `main.go`

### Want to Deploy on TrueNAS?
1. Read `docs/TRUENAS_DEPLOYMENT.md`
2. Choose deployment method (Docker Compose recommended)
3. Follow step-by-step guide
4. Integrate with existing qBittorrent setup

### Want to Extend It?
1. Understand the architecture (`PROJECT_SUMMARY.md`)
2. Code is modular - each component is independent
3. Add new matchers, parsers, or commands
4. See "Roadmap" in `README.md` for ideas

---

## 💡 Tips

1. **Start Small**: Test with one show first
2. **Use Review Mode**: Get comfortable before batch approvals
3. **Tune Gradually**: Start with basic rules, refine over time
4. **Check Logs**: Especially when testing automation
5. **Backup Database**: It's just a SQLite file - easy to backup

---

## 🐛 Troubleshooting

### Can't connect to qBittorrent
→ Run `curator test` to diagnose
→ Check Web UI is enabled in qBittorrent settings

### No matches found
→ Check your `SHOW_NAMES` spelling
→ Try with no filters temporarily
→ Review RSS feed item titles

### Build errors
→ Ensure Go 1.22+ installed
→ Ensure gcc available (for SQLite)
→ Check `go.mod` dependencies

**Full troubleshooting**: See `README.md` → Troubleshooting

---

## 🔐 Security Notes

- **RSS Feed URL**: Contains your passkey - keep `.env` file secure
- **qBittorrent Credentials**: Use environment variables or secrets
- **Database**: No sensitive data stored (just torrent metadata)
- **Network**: Consider running through VPN (like your Gluetun setup)

---

## 📊 Stats

- **Language**: Go 1.22
- **Lines of Code**: ~2,000+ (source)
- **Dependencies**: 3 (qBittorrent client, SQLite driver, zap logger)
- **Build Time**: <10 seconds
- **Memory**: ~15 MB runtime
- **Status**: ✅ Production ready

---

## 🎯 Next Steps

### Right Now
1. Open `README.md` — Installation and Configuration sections
2. Follow the build and setup steps
3. Run your first check
4. Approve your first torrent

### This Week
1. Set up automation (cron or systemd)
2. Tune your matching rules
3. Monitor for a few days
4. Adjust quality/group preferences

### This Month
1. Consider TrueNAS deployment
2. Add webhook notifications (future)
3. Build statistics dashboard (future)
4. Share with friends!

---

## 📞 Need Help?

1. Check the relevant doc:
   - Getting started? → `README.md` — Installation & Configuration
   - Configuration issues? → `README.md` → Troubleshooting
   - TrueNAS deployment? → `docs/TRUENAS_DEPLOYMENT.md`
   - Architecture/internals? → `docs/ARCHITECTURE.md`
   - Technical details? → `docs/PROJECT_SUMMARY.md`

2. Common issues are in `README.md` → Troubleshooting

3. Source code is well-commented - check internals

---

## 🎉 You're Ready!

Everything you need is in this directory. Pick your path:

- **Quick Start**: `README.md` → Installation & Configuration
- **Full Reference**: `README.md` → complete guide
- **Architecture**: `docs/ARCHITECTURE.md` → diagrams
- **Production Deploy**: `docs/TRUENAS_DEPLOYMENT.md` → 30 minutes
- **Deep Dive**: `docs/PROJECT_SUMMARY.md` → all components

**Most Important**: Just start! Build it, test it, use it.

---

**Built with ❤️ in Go**

Enjoy your automated torrent curator! 🎬
