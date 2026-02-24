# Container Setup - Quick Reference

## Files Added

| File | Purpose |
|------|---------|
| `Dockerfile` | Multi-stage Docker build configuration |
| `.dockerignore` | Excludes files from Docker build context |
| `docker-compose.yml` | Local development and testing setup |
| `.github/workflows/build-and-push.yml` | GitHub Actions for GHCR publishing |
| `CONTAINER_GUIDE.md` | Comprehensive container documentation |
| `CONTAINER_SETUP.md` | This setup summary |
| `.github/DEPLOYMENT.md` | Deployment strategies and guides |

## Files Modified

| File | Changes |
|------|---------|
| `Makefile` | Added `docker-build`, `docker-run`, `docker-push`, `docker-clean` targets |
| `README.md` | Added Docker installation section, badges, container features |

## Quick Commands

### Local Development (Recommended)

```bash
# Setup
cp curator.env.sample .env
vim .env  # Configure

# Start
docker-compose up -d
docker-compose logs -f

# Execute
docker-compose exec curator /app/curator check
docker-compose exec curator /app/curator list

# Stop
docker-compose down
```

### Build & Run Manually

```bash
# Build
docker build -t rss-curator:latest .

# Run check
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check

# Or use Make
make docker-build
make docker-run
make docker-clean
```

### GitHub Container Registry

GitHub Actions automatically publishes to `ghcr.io/iillmaticc/rss-curator` when you:
- Push to `main` or `develop` branch
- Create a git tag (e.g., `v0.1.0`)

No manual steps needed!

## Docker Image Info

- **Base**: Alpine Linux
- **Size**: ~30-40MB
- **Go Version**: 1.22
- **Database**: SQLite (persistent volume)

## Deployment Options

1. **Local Dev** → Docker Compose
2. **Self-hosted** → Docker + cron
3. **TrueNAS** → Pull from GHCR, deploy as custom app
4. **Production** → Docker with Kubernetes/orchestration

## Important Notes

⚠️ **Before using:**
1. Copy `curator.env.sample` to `.env`
2. Configure RSS feed URL, qBittorrent credentials, and show names
3. qBittorrent must be accessible at configured URL

📚 **Documentation:**
- Quick setup: This file
- Full guide: [CONTAINER_GUIDE.md](./CONTAINER_GUIDE.md)
- Deployment: [.github/DEPLOYMENT.md](./.github/DEPLOYMENT.md)
- TrueNAS: [TRUENAS_DEPLOYMENT.md](./TRUENAS_DEPLOYMENT.md)

## Next Steps

1. **Test locally**: `docker-compose up -d`
2. **Configure**: Edit `.env` with your settings
3. **Push to GitHub**: Automatic GHCR publishing activates
4. **Deploy to TrueNAS**: Pull from GHCR registry

## Troubleshooting

**Container won't connect to qBittorrent:**
- Check `QBITTORRENT_HOST` in `.env`
- If using host network: `docker run --network host`
- If using bridge: Use container name (e.g., `qbittorrent:8080`)

**Permission denied on database:**
```bash
docker-compose down
docker volume rm curator-data
docker-compose up -d
```

**Want to see logs?**
```bash
docker-compose logs -f
```

---

For detailed documentation, see the files listed above. Happy containerizing! 🐳
