# Deploying RSS Curator on TrueNAS

This guide covers deploying RSS Curator as a containerized application on TrueNAS SCALE, integrated with your existing qBittorrent setup.

## Architecture

```
TrueNAS SCALE
├── qBittorrent (container via Gluetun VPN)
└── RSS Curator (new container)
    ├── Scheduled checks via cron
    └── Accesses qBittorrent via container networking
```

## Prerequisites

- TrueNAS SCALE 25.10.0.1 or later
- qBittorrent already running (via your Gluetun VPN setup)
- qBittorrent Web UI accessible
- RSS feed URL from your private tracker

---

## Option 1: Docker Compose (Recommended)

### 1. Create Project Directory

```bash
mkdir -p /mnt/cell_block_d/apps/rss-curator
cd /mnt/cell_block_d/apps/rss-curator
```

### 2. Create Dockerfile

Save as `/mnt/cell_block_d/apps/rss-curator/Dockerfile`:

```dockerfile
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=1 go build -o curator ./cmd/curator

# Final stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app

COPY --from=builder /app/curator /usr/local/bin/curator

# Create directory for database
RUN mkdir -p /data

ENTRYPOINT ["curator"]
CMD ["check"]
```

### 3. Create docker-compose.yaml

Save as `/mnt/cell_block_d/apps/rss-curator/docker-compose.yaml`:

```yaml
version: '3.8'

services:
  rss-curator:
    build: .
    container_name: rss-curator
    restart: unless-stopped
    
    environment:
      # RSS Feed
      RSS_FEED_URL: "${RSS_FEED_URL}"
      
      # qBittorrent connection
      QBITTORRENT_HOST: "http://qbittorrent:8080"
      QBITTORRENT_USER: "${QBITTORRENT_USER}"
      QBITTORRENT_PASS: "${QBITTORRENT_PASS}"
      QBITTORRENT_CATEGORY: "curator"
      
      # Show preferences
      SHOW_NAMES: "${SHOW_NAMES}"
      MIN_QUALITY: "1080p"
      PREFERRED_CODEC: "x265"
      PREFERRED_GROUPS: "NTb,FLUX,HMAX,CMRG"
      
      # Storage
      STORAGE_PATH: "/data/curator.db"
    
    volumes:
      - ./data:/data
    
    # Connect to qBittorrent's network
    network_mode: "container:qbittorrent"
    
    # Run check every 30 minutes
    command: sh -c "while true; do curator check; sleep 1800; done"

  # Optional: CLI access container
  curator-cli:
    build: .
    container_name: curator-cli
    restart: "no"
    
    environment:
      RSS_FEED_URL: "${RSS_FEED_URL}"
      QBITTORRENT_HOST: "http://qbittorrent:8080"
      QBITTORRENT_USER: "${QBITTORRENT_USER}"
      QBITTORRENT_PASS: "${QBITTORRENT_PASS}"
      STORAGE_PATH: "/data/curator.db"
    
    volumes:
      - ./data:/data
    
    network_mode: "container:qbittorrent"
    
    entrypoint: ["/bin/sh"]
    stdin_open: true
    tty: true
```

### 4. Create .env File

Save as `/mnt/cell_block_d/apps/rss-curator/.env`:

```bash
# RSS Feed URL (get from your tracker's RSS settings)
RSS_FEED_URL=https://your-tracker.com/rss?passkey=YOUR_PASSKEY

# qBittorrent credentials
QBITTORRENT_USER=admin
QBITTORRENT_PASS=your-password

# Shows to watch (comma-separated)
SHOW_NAMES=The Last of Us,Foundation,Severance,House of the Dragon
```

### 5. Build and Run

```bash
# Build the container
docker-compose build

# Start the automated checker
docker-compose up -d rss-curator

# Check logs
docker-compose logs -f rss-curator
```

### 6. Manual Operations

Use the CLI container for manual approvals:

```bash
# Start CLI container
docker-compose run --rm curator-cli

# Inside container:
curator list              # List pending
curator approve 1 2 3     # Approve specific IDs
curator review            # Interactive mode
curator test              # Test connections
exit
```

---

## Option 2: TrueNAS Custom App

### 1. Prepare Files

Copy the project to your TrueNAS apps directory:

```bash
scp -r rss-curator user@truenas:/mnt/cell_block_d/apps/
```

### 2. Build Container

SSH into TrueNAS and build:

```bash
ssh user@truenas
cd /mnt/cell_block_d/apps/rss-curator
docker build -t rss-curator:latest .
```

### 3. Create TrueNAS App

In TrueNAS web UI:
1. Apps → Discover Apps → Custom App
2. Configure:
   - **Application Name**: rss-curator
   - **Image Repository**: rss-curator
   - **Image Tag**: latest
   - **Container Entrypoint**: `sh -c "while true; do curator check; sleep 1800; done"`
   
3. Environment Variables:
   ```
   RSS_FEED_URL=your-feed-url
   QBITTORRENT_HOST=http://qbittorrent:8080
   QBITTORRENT_USER=admin
   QBITTORRENT_PASS=password
   SHOW_NAMES=Show 1,Show 2
   STORAGE_PATH=/data/curator.db
   ```

4. Storage:
   - Host Path: `/mnt/cell_block_d/apps/rss-curator/data`
   - Mount Path: `/data`

5. Networking:
   - Use same network as qBittorrent container

---

## Option 3: Systemd Service (Native)

If you prefer running natively on TrueNAS without containers:

### 1. Copy Binary

```bash
scp curator user@truenas:/tmp/
ssh user@truenas 'sudo cp /tmp/curator /usr/local/bin/ && sudo chmod +x /usr/local/bin/curator'
```

### 2. Create Config

SSH into TrueNAS:

```bash
sudo mkdir -p /etc/rss-curator
sudo vim /etc/rss-curator/config.env
```

Add your configuration (same as .env above).

### 3. Create Systemd Service

Save as `/etc/systemd/system/rss-curator.service`:

```ini
[Unit]
Description=RSS Curator Check
After=network.target

[Service]
Type=oneshot
User=apps
EnvironmentFile=/etc/rss-curator/config.env
ExecStart=/usr/local/bin/curator check
StandardOutput=journal
StandardError=journal
```

### 4. Create Timer

Save as `/etc/systemd/system/rss-curator.timer`:

```ini
[Unit]
Description=RSS Curator Timer
Requires=rss-curator.service

[Timer]
OnBootSec=5min
OnUnitActiveSec=30min

[Install]
WantedBy=timers.target
```

### 5. Enable and Start

```bash
sudo systemctl enable rss-curator.timer
sudo systemctl start rss-curator.timer
sudo systemctl status rss-curator.timer
```

---

## Network Configuration

Since qBittorrent runs through Gluetun VPN, you have two options:

### Option A: Share qBittorrent's Network (Recommended)

```yaml
network_mode: "container:qbittorrent"
```

This puts RSS Curator in the same network namespace as qBittorrent, allowing direct localhost access.

### Option B: Docker Network

```yaml
networks:
  - apps

networks:
  apps:
    external: true
```

Connect both qBittorrent and RSS Curator to a shared Docker network.

---

## Accessing the CLI

### Via Docker Compose

```bash
cd /mnt/cell_block_d/apps/rss-curator
docker-compose run --rm curator-cli
```

### Via Direct Container

```bash
docker exec -it rss-curator curator list
docker exec -it rss-curator curator review
```

### Via SSH (if native install)

```bash
ssh user@truenas
source /etc/rss-curator/config.env
curator list
curator review
```

---

## Monitoring

### Check Logs

```bash
# Docker Compose
docker-compose logs -f rss-curator

# TrueNAS App
# View in Apps UI or via kubectl

# Systemd
sudo journalctl -u rss-curator.service -f
```

### Check Database

```bash
# Access the data directory
cd /mnt/cell_block_d/apps/rss-curator/data

# Query directly
sqlite3 curator.db "SELECT COUNT(*) FROM staged_torrents WHERE status='pending';"
```

---

## Maintenance

### Update Container

```bash
cd /mnt/cell_block_d/apps/rss-curator
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Backup Database

```bash
cp /mnt/cell_block_d/apps/rss-curator/data/curator.db \
   /mnt/cell_block_d/apps/rss-curator/data/curator.db.backup
```

### Clean Old Entries

```bash
docker-compose run --rm curator-cli
# Inside container:
# curator clean --older-than 30d  # Future feature
```

---

## Troubleshooting

### Can't Connect to qBittorrent

1. Check qBittorrent is running:
   ```bash
   docker ps | grep qbittorrent
   ```

2. Test from curator container:
   ```bash
   docker-compose run --rm curator-cli
   curator test
   ```

3. Verify network mode is correct

### No Matches Found

1. Check RSS feed is accessible:
   ```bash
   curl -I "$RSS_FEED_URL"
   ```

2. Debug matching:
   ```bash
   docker-compose run --rm curator-cli
   curator check
   ```

3. Review SHOW_NAMES in .env

### Database Locked

SQLite doesn't handle concurrent writes well. Ensure only one curator instance is running.

---

## Security

1. **Protect .env file**:
   ```bash
   chmod 600 /mnt/cell_block_d/apps/rss-curator/.env
   ```

2. **Use secrets** (advanced):
   ```yaml
   secrets:
     qbit_pass:
       file: ./secrets/qbit_pass
   ```

3. **Network isolation**: Keep curator in qBittorrent's VPN network

---

## Performance

Expected resource usage:
- **CPU**: <5% during checks, idle otherwise
- **Memory**: 10-20 MB
- **Disk**: <1 MB database (grows with history)
- **Network**: Minimal (only RSS feeds + qBittorrent API)

---

## Integration with Existing Setup

Since you already have:
- qBittorrent running via Gluetun
- nginx reverse proxy
- Organized docker-compose files

This fits naturally:

```
/mnt/cell_block_d/apps/
├── gluetun/
│   └── docker-compose.yaml
├── qbittorrent/
│   └── docker-compose.yaml
├── nginx/
│   └── docker-compose.yaml
└── rss-curator/          ← New
    ├── docker-compose.yaml
    ├── Dockerfile
    ├── .env
    └── data/
        └── curator.db
```

All following the same organizational pattern you've established.

---

## Recommended Setup

For your use case, I recommend:

1. **Docker Compose** deployment (familiar pattern)
2. **Share qBittorrent's network** (simplest connectivity)
3. **30-minute check interval** (balanced)
4. **Use Tailscale** for remote CLI access

This gives you:
- Consistent with your existing infrastructure
- Easy to maintain
- Secure (VPN-routed)
- Remote access via Tailscale

---

## Questions?

Feel free to adjust the configuration to match your preferences. The modular design makes it easy to tweak timing, matching rules, and deployment style.

Happy curating! 🎬
