# Docker Container Guide

This guide covers how to build, run, and deploy RSS Curator in Docker containers.

## Table of Contents

1. [Quick Start](#quick-start)
2. [Building Locally](#building-locally)
3. [Configuration](#configuration)
4. [Running with Docker](#running-with-docker)
5. [Running with Docker Compose](#running-with-docker-compose)
6. [GitHub Container Registry](#github-container-registry)
7. [TrueNAS Deployment](#truenas-deployment)
8. [Networking](#networking)
9. [Troubleshooting](#troubleshooting)

## Quick Start

### Using Docker (Pre-built image)

```bash
# Pull the latest image from GitHub Container Registry
docker pull ghcr.io/iillmaticc/rss-curator:latest

# Create your configuration (copy from sample)
cp curator.env.sample .env

# Edit with your settings
vim .env

# Run the container
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  ghcr.io/iillmaticc/rss-curator:latest check
```

### Using Docker Compose (Recommended for local development)

```bash
# Copy configuration
cp curator.env.sample .env

# Edit with your settings
vim .env

# Build and run
docker-compose up -d

# View logs
docker-compose logs -f

# Stop
docker-compose down
```

## Building Locally

### Prerequisites

- Docker or Docker Desktop installed
- Go 1.22 or later (for local development)

### Build the Docker Image

```bash
# Build with default tag
docker build -t rss-curator:latest .

# Or with a specific tag
docker build -t rss-curator:v0.1.0 .

# Build with BuildKit for better caching
DOCKER_BUILDKIT=1 docker build -t rss-curator:latest .
```

## Configuration

### Using Environment Variables

All configuration is done through environment variables. You have several options:

#### Option 1: Direct environment variables

```bash
docker run --rm \
  -e RSS_FEED_URL="https://tracker.com/rss?passkey=xxx" \
  -e QBITTORRENT_HOST="http://host.docker.internal:8080" \
  -e QBITTORRENT_USER="admin" \
  -e QBITTORRENT_PASS="password" \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check
```

#### Option 2: Using .env file

```bash
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check
```

#### Option 3: Using docker-compose

```bash
# Edit docker-compose.yml or create .env file
docker-compose up
```

### Required Environment Variables

- `RSS_FEED_URL` - Your private tracker RSS feed URL with passkey
- `QBITTORRENT_HOST` - qBittorrent Web UI URL (e.g., `http://localhost:8080`)
- `QBITTORRENT_USER` - qBittorrent username
- `QBITTORRENT_PASS` - qBittorrent password

### Optional Environment Variables

See [curator.env.sample](./curator.env.sample) for all available options:

- `QBITTORRENT_CATEGORY` - Category for downloads (default: curator)
- `SHOW_NAMES` - Comma-separated show names to match
- `MIN_QUALITY` - Minimum quality (720p, 1080p, 2160p)
- `PREFERRED_CODEC` - Preferred codec (x264, x265, HEVC)
- `PREFERRED_GROUPS` - Preferred release groups
- `EXCLUDE_GROUPS` - Release groups to exclude
- `STORAGE_PATH` - Path to SQLite database (default: /app/data/curator.db)

## Running with Docker

### Single command execution

```bash
# Check for new releases
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check

# List staged releases
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest list

# Add a new feed
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest add https://tracker.com/rss?passkey=xxx
```

### Interactive container

```bash
docker run -it \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest /bin/sh
```

## Running with Docker Compose

### Start the service

```bash
docker-compose up -d
```

### View logs

```bash
# Follow logs
docker-compose logs -f

# View only error logs
docker-compose logs -f | grep ERROR
```

### Execute commands

```bash
# Check for new releases
docker-compose exec curator /app/curator check

# List staged releases
docker-compose exec curator /app/curator list

# List with verbose output
docker-compose exec curator /app/curator list -v
```

### Stop the service

```bash
# Stop but keep volumes
docker-compose stop

# Stop and remove containers
docker-compose down

# Stop and remove everything including volumes
docker-compose down -v
```

### Update the image

```bash
# Pull latest changes and rebuild
docker-compose down
docker-compose pull
docker-compose up -d --build
```

## GitHub Container Registry

### Automatic Publishing

The project uses GitHub Actions to automatically build and push images to GitHub Container Registry (GHCR) on:

- Pushes to `main` or `develop` branches
- New tags matching `v*` (e.g., `v0.1.0`, `v1.0.0`)
- Pull requests (builds only, no push)

### Available Images

Images are automatically tagged with:

- `latest` - Latest build from main branch
- Branch names - e.g., `main`, `develop`
- Semantic versions - e.g., `v0.1.0`, `0.1`, `0`
- Commit SHA - e.g., `main-a1b2c3d`

### Pulling from GHCR

```bash
# Authenticate with your GitHub token (one-time setup)
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin

# Pull the image
docker pull ghcr.io/iillmaticc/rss-curator:latest
```

### Authentication for Private Repositories

If your repository is private, you need to authenticate:

```bash
# Using personal access token with read:packages scope
docker login ghcr.io

# Or use environment variable
export CR_PAT=YOUR_GITHUB_TOKEN
echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin
```

## TrueNAS Deployment

### Using Docker Image in TrueNAS

1. **Pull the image in TrueNAS**:
   - Go to Apps > Discover > Container Images
   - Search for `ghcr.io/iillmaticc/rss-curator`
   - Pull the desired version

2. **Create a new App**:
   - Create new Docker container with the pulled image
   - Configure environment variables (see Configuration section)
   - Mount volumes for persistent storage

3. **Configure Volumes**:
   - Mount `/app/data` to a persistent dataset for SQLite database
   - Example: `/mnt/pool/apps/curator/data:/app/data`

4. **Networking**:
   - If qBittorrent is on the same host, use `host` network mode
   - If on a different host, use `bridge` and set `QBITTORRENT_HOST` appropriately

5. **Scheduling**:
   - Use cron or TrueNAS scheduling to run periodic checks
   - Example: Daily check at 2 AM

### Example TrueNAS Custom App

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rss-curator
spec:
  containers:
  - name: rss-curator
    image: ghcr.io/iillmaticc/rss-curator:latest
    env:
    - name: RSS_FEED_URL
      valueFrom:
        secretKeyRef:
          name: curator-config
          key: feed-url
    - name: QBITTORRENT_HOST
      value: "http://qbittorrent:8080"
    - name: QBITTORRENT_USER
      valueFrom:
        secretKeyRef:
          name: curator-config
          key: qb-user
    - name: QBITTORRENT_PASS
      valueFrom:
        secretKeyRef:
          name: curator-config
          key: qb-pass
    volumeMounts:
    - name: data
      mountPath: /app/data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: curator-data
```

## Networking

### Host Network

Used for direct access to qBittorrent on localhost:

```bash
docker run --network host ...
```

**Pros**: Direct access to host services
**Cons**: Container shares host network namespace

### Bridge Network

Used for container-to-container communication:

```bash
docker run --network bridge ...
```

**Pros**: Isolated network, better security
**Cons**: Need to use container names as hostnames

### Docker Compose with Multiple Services

If running qBittorrent in the same compose file:

```yaml
services:
  curator:
    environment:
      QBITTORRENT_HOST: "http://qbittorrent:8080"
  qbittorrent:
    # qBittorrent service definition
```

## Troubleshooting

### Container won't start

Check logs:
```bash
docker logs <container_id>
```

Common issues:
- Missing or invalid environment variables
- Database file permissions
- qBittorrent not accessible

### Cannot connect to qBittorrent

```bash
# If using host network, verify qBittorrent is running
netstat -an | grep 8080

# If using bridge network, check hostname resolution
docker exec <container> ping qbittorrent

# Test connectivity
docker exec <container> curl -s http://qbittorrent:8080/api/v2/app/version
```

### Database file permission denied

```bash
# Fix volume permissions
docker exec <container> chown -R app:app /app/data

# Or rebuild from scratch
docker-compose down -v
docker-compose up -d
```

### Out of disk space

Check volume usage:
```bash
docker volume ls
docker volume inspect curator-data
```

### Performance issues

- Check resource limits in docker-compose.yml
- Monitor container resource usage: `docker stats`
- Check qBittorrent connectivity and response time

## Image Details

- **Base Image**: Alpine Linux (minimal, ~13MB base)
- **Go Version**: 1.22
- **Binary Size**: ~10-15MB (stripped)
- **Final Image Size**: ~30-40MB

## Security Considerations

- Never commit `.env` files with secrets
- Use Docker secrets or environment variables
- Run with least privileges
- Keep images updated
- Use specific version tags in production, not `latest`

## Next Steps

See [TRUENAS_DEPLOYMENT.md](./TRUENAS_DEPLOYMENT.md) for detailed TrueNAS setup instructions.
