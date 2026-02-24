# Deployment Guide

This document outlines deployment strategies for RSS Curator.

## Quick Links

- **Local Development**: See [QUICKSTART.md](../QUICKSTART.md)
- **Container Guide**: See [CONTAINER_GUIDE.md](../CONTAINER_GUIDE.md)
- **TrueNAS Deployment**: See [TRUENAS_DEPLOYMENT.md](../TRUENAS_DEPLOYMENT.md)

## Deployment Options

### Option 1: Host Installation (Linux/macOS)

Best for: Development, simple setups

```bash
# Build from source
git clone https://github.com/iillmaticc/rss-curator
cd rss-curator
go build -o curator ./cmd/curator

# Install
sudo cp curator /usr/local/bin/

# Configure
cp curator.env.sample ~/.curator.env
vim ~/.curator.env

# Run
curator check
```

### Option 2: Docker Container

Best for: Isolation, reproducibility, easy updates

```bash
# Build
docker build -t rss-curator:latest .

# Configure
cp curator.env.sample .env
vim .env

# Run
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check
```

### Option 3: Docker Compose

Best for: Local development, testing, self-hosted deployments

```bash
# Configure
cp curator.env.sample .env
vim .env

# Start
docker-compose up -d

# Monitor
docker-compose logs -f

# Execute commands
docker-compose exec curator /app/curator check
docker-compose exec curator /app/curator list
```

### Option 4: GitHub Container Registry + TrueNAS

Best for: Production, automated updates, cloud-native deployments

1. Push your repo to GitHub
2. GitHub Actions automatically builds and pushes to GHCR
3. Pull image in TrueNAS and deploy as custom app
4. Optionally schedule with cron or TrueNAS scheduler

See [CONTAINER_GUIDE.md#truenas-deployment](../CONTAINER_GUIDE.md#truenas-deployment) for details.

## Making Docker Builds

### Prerequisites

- Docker or Docker Desktop installed
- Docker BuildKit enabled (recommended)

### Build Commands

```bash
# Simple build
docker build -t rss-curator:latest .

# Build with BuildKit
DOCKER_BUILDKIT=1 docker build -t rss-curator:latest .

# Build with specific version tag
docker build -t rss-curator:v0.1.0 .

# Build for multiple platforms (requires buildx)
docker buildx build --platform linux/amd64,linux/arm64 -t rss-curator:latest .
```

### Using Make

```bash
# Build Docker image
make docker-build

# Build and run
make docker-run

# Clean up
make docker-clean
```

## Publishing to GitHub Container Registry

### Prerequisites

- GitHub account with push access to repository
- Personal access token with `write:packages` scope
- Docker authenticated with GHCR (one-time setup)

### Setup Authentication

```bash
# Create a personal access token at https://github.com/settings/tokens
# Select scope: write:packages (and read:packages for pulling)

# Authenticate
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin

# Or interactive
docker login ghcr.io
```

### Manual Publishing

```bash
# Build your image
docker build -t rss-curator:v0.1.0 .

# Tag for GHCR
docker tag rss-curator:v0.1.0 ghcr.io/iillmaticc/rss-curator:v0.1.0
docker tag rss-curator:v0.1.0 ghcr.io/iillmaticc/rss-curator:latest

# Push
docker push ghcr.io/iillmaticc/rss-curator:v0.1.0
docker push ghcr.io/iillmaticc/rss-curator:latest
```

### Automatic Publishing (GitHub Actions)

The project includes a GitHub Actions workflow (`.github/workflows/build-and-push.yml`) that automatically:

- Builds on every push to `main` or `develop`
- Builds on every pull request (no push)
- Automatically tags with branch names, versions, and git SHA
- Pushes to GHCR on successful build
- Caches layers for faster builds

Simply push your code to GitHub and the workflow runs automatically!

### Image Tags

The GitHub Actions workflow creates multiple tags:

- `latest` - Latest from main branch
- `main`, `develop` - Branch names
- `v0.1.0` - Semantic versions (from git tags)
- `0.1`, `0` - Major/minor versions
- `main-a1b2c3d` - Commit SHA

## Scheduling

### Cron-based Execution

**Linux/macOS:**

```bash
# Edit crontab
crontab -e

# Run check every 6 hours
0 0,6,12,18 * * * source ~/.curator.env && /usr/local/bin/curator check

# Or with docker
0 0,6,12,18 * * * docker run --rm --env-file /path/to/.env --network host -v curator-data:/app/data ghcr.io/iillmaticc/rss-curator:latest check
```

**Docker Compose:**

```bash
# Use a cron container or host cron
0 0,6,12,18 * * * cd /path/to/rss-curator && docker-compose exec -T curator /app/curator check
```

**TrueNAS:**

Use the TrueNAS UI to schedule periodic execution of the container.

### Systemd Timer (Linux)

See [QUICKSTART.md](../QUICKSTART.md) for detailed instructions.

## Monitoring and Logging

### Local Installation

```bash
# View logs (depends on how you run it)
journalctl -u curator.service -f  # If using systemd timer

# Or run directly with logging
curator check 2>&1 | tee -a ~/.curator.log
```

### Docker

```bash
# View container logs
docker logs -f <container_name>

# Docker Compose
docker-compose logs -f
docker-compose logs -f curator

# With timestamps
docker-compose logs -f --timestamps
```

### TrueNAS

Check logs in the TrueNAS UI or access container logs through TrueNAS app management.

## Updating

### Host Installation

```bash
# Pull latest changes
cd /path/to/rss-curator
git pull origin main

# Rebuild
make build
make install

# Restart service (if using systemd)
sudo systemctl restart curator.service
```

### Docker Image

```bash
# Pull latest
docker pull ghcr.io/iillmaticc/rss-curator:latest

# Rebuild locally
docker build -t rss-curator:latest . --no-cache

# Restart container
docker-compose down
docker-compose up -d
```

### TrueNAS

Check for updates in the TrueNAS Apps UI or manually pull latest image in the container management section.

## Backup and Recovery

### Backing up Database

```bash
# Docker: Copy database from volume
docker run --rm -v curator-data:/app/data -v $(pwd):/backup alpine \
  cp /app/data/curator.db /backup/curator.db.backup

# Docker Compose
docker-compose exec curator cp /app/data/curator.db /app/data/curator.db.backup

# Host
cp ~/.curator.db ~/.curator.db.backup
```

### Restoring Database

```bash
# Docker
docker run --rm -v curator-data:/app/data -v $(pwd):/backup alpine \
  cp /backup/curator.db.backup /app/data/curator.db

# Host
cp ~/.curator.db.backup ~/.curator.db
```

## Troubleshooting

### Container won't start

```bash
# Check logs
docker logs <container_id>
docker-compose logs

# Verify environment variables
docker-compose config | grep -E "RSS_FEED|QBITTORRENT"

# Test with minimal config
docker run --rm -it --entrypoint /bin/sh rss-curator:latest
```

### Can't connect to qBittorrent

```bash
# Check if qBittorrent is accessible
curl http://localhost:8080/api/v2/app/version

# From container
docker-compose exec curator curl -v http://qbittorrent:8080/api/v2/app/version

# Test with host network
docker run --rm --network host --entrypoint ping rss-curator:latest localhost
```

### Database permission issues

```bash
# Fix permissions
docker-compose down
docker volume rm curator-data
docker-compose up -d
```

## Performance Tips

1. **Use specific version tags in production**, not `latest`
2. **Monitor resource usage**: `docker stats`
3. **Use multi-stage builds** for smaller images
4. **Enable BuildKit** for faster builds: `DOCKER_BUILDKIT=1`
5. **Use Alpine Linux** base for minimal image size
6. **Implement health checks** for containers
7. **Set appropriate restart policies**

## Security Considerations

- Never commit `.env` files with secrets to git
- Use Docker secrets or environment variables for sensitive data
- Keep images updated with security patches
- Use read-only volumes where possible
- Run containers with minimal privileges
- Consider using a private registry for sensitive deployments
- Use network policies to restrict container communication

## Additional Resources

- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [GitHub Container Registry Docs](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [TrueNAS Apps Documentation](https://www.truenas.com/docs/)
