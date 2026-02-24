# Container & GHCR Setup Summary

This document summarizes the containerization and GitHub Container Registry (GHCR) integration added to RSS Curator.

## Files Created/Modified

### New Files

1. **Dockerfile** - Multi-stage Docker build configuration
   - Builds optimized Go binary in builder stage
   - Runtime image based on Alpine Linux (~30-40MB final size)
   - Includes SQLite support and security best practices

2. **.dockerignore** - Excludes unnecessary files from Docker build context
   - Reduces build context size and image bloat
   - Excludes git, IDE files, build artifacts, etc.

3. **docker-compose.yml** - Local development and testing
   - Easy one-command setup: `docker-compose up -d`
   - Pre-configured environment variable handling
   - Persistent volume for SQLite database
   - Service restart and logging options

4. **.github/workflows/build-and-push.yml** - GitHub Actions CI/CD
   - Automatically builds on pushes to main/develop and pull requests
   - Pushes images to GitHub Container Registry (GHCR)
   - Intelligent tagging strategy (version, branch, SHA)
   - Docker build caching for faster builds

5. **CONTAINER_GUIDE.md** - Comprehensive container documentation
   - Quick start guide
   - Building, running, and configuring containers
   - GHCR usage and authentication
   - TrueNAS deployment instructions
   - Networking and troubleshooting guides

6. **.github/DEPLOYMENT.md** - Deployment strategies and best practices
   - Multiple deployment options (host, Docker, compose, GHCR+TrueNAS)
   - Publishing to GHCR guide
   - Scheduling and automation
   - Backup/recovery procedures
   - Performance and security tips

### Modified Files

1. **Makefile** - Added Docker targets
   - `make docker-build` - Build Docker image
   - `make docker-run` - Build and run with .env config
   - `make docker-push` - Push to registry
   - `make docker-clean` - Remove image

2. **README.md** - Updated with container information
   - Added build badges (Actions, Container, Go version)
   - Added Docker installation section with 3 options
   - Linked to CONTAINER_GUIDE.md for detailed docs
   - Updated feature list to mention Docker and TrueNAS

## Quick Start

### For Local Development

```bash
# Copy environment config
cp curator.env.sample .env
vim .env  # Configure with your settings

# Build and run with Docker Compose
docker-compose up -d

# View logs
docker-compose logs -f

# Run commands
docker-compose exec curator /app/curator check
docker-compose exec curator /app/curator list
```

### Building the Docker Image

```bash
# Build locally
docker build -t rss-curator:latest .

# Or use Make
make docker-build

# Run
docker run --rm \
  --env-file .env \
  --network host \
  -v curator-data:/app/data \
  rss-curator:latest check
```

### Publishing to GitHub Container Registry

The GitHub Actions workflow handles this automatically:

1. Push code to GitHub (`main`, `develop`, or create a tag)
2. GitHub Actions automatically builds the image
3. Image is pushed to `ghcr.io/iillmaticc/rss-curator`

For manual publishing or private repos, see [.github/DEPLOYMENT.md](.github/DEPLOYMENT.md#publishing-to-github-container-registry).

## GitHub Actions Workflow

The workflow (`.github/workflows/build-and-push.yml`) runs on:

- ✅ Pushes to `main` branch (tags as `latest`)
- ✅ Pushes to `develop` branch
- ✅ New git tags matching `v*` (e.g., `v0.1.0`)
- ✅ Pull requests (builds only, doesn't push)

**Image tags created:**
- `latest` - Latest from main branch
- `main`, `develop` - Branch names
- `v0.1.0` - Semantic versions from tags
- `main-a1b2c3d` - Commit SHA for tracking

## TrueNAS Integration

The container setup is optimized for TrueNAS deployment:

1. **Lightweight Alpine Linux base** - Minimal resource usage
2. **SQLite database volume support** - Persistent storage
3. **Environment variable configuration** - TrueNAS-friendly
4. **Easy scheduling** - Run periodic checks via TrueNAS scheduler
5. **Pre-built images from GHCR** - No build step required in TrueNAS

See [CONTAINER_GUIDE.md#truenas-deployment](./CONTAINER_GUIDE.md#truenas-deployment) and [TRUENAS_DEPLOYMENT.md](./TRUENAS_DEPLOYMENT.md) for detailed TrueNAS setup.

## Image Details

- **Base Image**: Alpine Linux (minimal, ~13MB)
- **Go Version**: 1.22
- **Build Tool**: Multi-stage Docker build
- **Binary Size**: ~10-15MB (statically linked)
- **Final Image Size**: ~30-40MB total
- **Optimization**: Layer caching, minimal dependencies

## Key Features

✅ **Automatic GHCR Publishing** - GitHub Actions builds and publishes automatically
✅ **Quick Local Development** - `docker-compose up -d` to start
✅ **Multi-platform Support** - Can build for multiple architectures
✅ **Persistent Storage** - SQLite database persists across restarts
✅ **Easy Configuration** - Environment variables via .env file
✅ **TrueNAS Ready** - Optimized for TrueNAS deployment
✅ **Build Caching** - GitHub Actions caches layers for fast rebuilds
✅ **Security** - Runs as non-root, minimal dependencies

## Next Steps

1. **Test locally**: `docker-compose up -d`
2. **Configure**: Copy and edit `.env` file
3. **Push to GitHub**: GitHub Actions will build and publish automatically
4. **Deploy to TrueNAS**: Pull from GHCR and create custom app

## Documentation Files

- [CONTAINER_GUIDE.md](./CONTAINER_GUIDE.md) - Complete container guide
- [.github/DEPLOYMENT.md](./.github/DEPLOYMENT.md) - Deployment strategies
- [TRUENAS_DEPLOYMENT.md](./TRUENAS_DEPLOYMENT.md) - TrueNAS specific setup
- [README.md](./README.md) - Project overview
- [QUICKSTART.md](./QUICKSTART.md) - Getting started guide

## Support

For issues or questions:
1. Check [CONTAINER_GUIDE.md](./CONTAINER_GUIDE.md#troubleshooting) troubleshooting section
2. Review [.github/DEPLOYMENT.md](./.github/DEPLOYMENT.md)
3. Open an issue on GitHub

---

Last updated: 2026-02-23
