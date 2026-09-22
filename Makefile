.PHONY: build clean install test help run \
        dev-up dev-down dev-logs dev-rebuild dev-clean \
        image-build image-push image-clean \
        rc-release \
        uat-up uat-down uat-logs uat-validate \
        test-e2e validate-smoke

BINARY_NAME=curator
INSTALL_PATH=/usr/local/bin
IMAGE_NAME=rss-curator
REGISTRY=ghcr.io/killakam3084
IMAGE=$(REGISTRY)/$(IMAGE_NAME)

# Container runtime — defaults to podman, override with: make dev-up CTR=docker
CTR ?= podman

help:
	@echo "RSS Curator - Makefile"
	@echo ""
	@echo "Local dev (Podman / OCI-compatible):"
	@echo "  make dev-up        Build image and start local stack (compose.dev.yml)"
	@echo "  make dev-down      Stop and remove local containers"
	@echo "  make dev-logs      Tail logs from the running dev container"
	@echo "  make dev-rebuild   Force image rebuild then restart"
	@echo "  make dev-clean     Stop containers and remove dev image"
	@echo ""
	@echo "Go targets:"
	@echo "  make build         Build the binary"
	@echo "  make install       Build and install to $(INSTALL_PATH)"
	@echo "  make test          Run tests"
	@echo "  make clean         Remove built binary"
	@echo "  make run           Build and run (requires ~/.curator.env)"
	@echo ""
	@echo "Image targets (production, push to GHCR):"
	@echo "  make image-build    Build OCI image"
	@echo "  make image-push     Push image to registry (requires auth)"
	@echo "  make image-clean    Remove local image"
	@echo ""
	@echo "Release candidates (validate before tagging a release):"
	@echo "  make rc-release     Build+push a multi-arch RC image tagged rc-<shortsha>"
	@echo "                      Requires: docker buildx create --use (one-time),"
	@echo "                      docker login ghcr.io (write:packages scope)"
	@echo ""
	@echo "Pre-promotion UAT (validate an RC/pinned tag before deploying):"
	@echo "  make uat-up REF=rc-<sha>|vX.Y.Z   Pull and run that exact image locally"
	@echo "  make uat-validate                 Run Hurl smoke+auth suite against it"
	@echo "  make uat-logs                      Tail logs from the running UAT stack"
	@echo "  make uat-down                      Stop and remove the UAT stack"
	@echo ""
	@echo "E2E / functional validation:"
	@echo "  make test-e2e       Build fresh stack + run smoke suite (CI)"
	@echo "  make validate-smoke Run smoke+auth tests against live stack (TrueNAS)"
	@echo "                      Requires: export CURATOR_USERNAME=... CURATOR_PASSWORD=..."
	@echo ""
	@echo "Override container runtime: make dev-up CTR=docker"

build:
	@echo "Building $(BINARY_NAME)..."
	go build -o $(BINARY_NAME) ./cmd/curator
	@echo "✓ Built: ./$(BINARY_NAME)"

install: build
	@echo "Installing to $(INSTALL_PATH)..."
	sudo cp $(BINARY_NAME) $(INSTALL_PATH)/
	@echo "✓ Installed: $(INSTALL_PATH)/$(BINARY_NAME)"

test:
	@echo "Running tests..."
	go test -v ./...

clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	@echo "✓ Cleaned"

run: build
	@if [ -f ~/.curator.env ]; then \
		echo "Loading config from ~/.curator.env"; \
		. ~/.curator.env && ./$(BINARY_NAME) check; \
	else \
		echo "Error: ~/.curator.env not found"; \
		echo "Copy curator.env.sample to ~/.curator.env and configure it"; \
		exit 1; \
	fi
# ── Local dev targets ────────────────────────────────────────────────────

dev-up:
	@if [ ! -f local.env ]; then \
		echo "Error: local.env not found"; \
		echo "Copy local.env.sample to local.env and configure it"; \
		exit 1; \
	fi
	@if [ ! -f shows.json ] && [ ! -f watchlist.json ]; then \
		cp watchlist.json.sample watchlist.json; \
		echo "info: copied watchlist.json.sample → watchlist.json"; \
	fi
	@mkdir -p data logs
	$(CTR) compose -f compose.dev.yml up -d
	@echo "✓ Dev stack running — API at http://localhost:8081"

dev-down:
	$(CTR) compose -f compose.dev.yml down

dev-logs:
	$(CTR) compose -f compose.dev.yml logs -f

dev-rebuild:
	$(CTR) compose -f compose.dev.yml up -d --build
	@echo "✓ Rebuilt and restarted"

dev-clean:
	$(CTR) compose -f compose.dev.yml down
	$(CTR) rmi rss-curator:dev 2>/dev/null || true
	@echo "✓ Dev containers and image removed"

# ── Production image targets ──────────────────────────────────────────────

image-build:
	@echo "Building image: $(IMAGE):latest"
	$(CTR) build -t $(IMAGE):latest .
	@echo "✓ Built: $(IMAGE):latest"

image-push:
	@echo "Pushing image to registry: $(IMAGE)"
	$(CTR) push $(IMAGE):latest
	@echo "✓ Pushed"

image-clean:
	$(CTR) rmi $(IMAGE):latest 2>/dev/null || true
	@echo "✓ Removed $(IMAGE):latest"

# ── Release candidate (validate locally before tagging a release) ────────

# rc-release: builds and pushes a multi-arch RC image tagged rc-<shortsha>,
# tied to the exact commit it was built from. CI later promotes this same
# manifest (no rebuild) once the corresponding vX.Y.Z tag is pushed — so the
# local UAT validation and the TrueNAS deploy always share identical bytes.
#
# Prerequisites (one-time):
#   docker buildx create --use
#   docker login ghcr.io   (PAT with write:packages)
#
# linux/amd64,linux/arm64 is required even for Mac-only testing: TrueNAS is
# amd64, so a plain arm64-only Mac build would ship the wrong arch.
RC_SHA := $(shell git rev-parse --short HEAD)

rc-release:
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: working tree is dirty — commit or stash changes before cutting an RC"; \
		exit 1; \
	fi
	@echo "Running local quality gates..."
	@test -z "$$(gofmt -l .)" || (echo "Error: gofmt found unformatted files — run 'gofmt -w .'"; exit 1)
	go vet ./...
	go test ./...
	@echo "Building and pushing $(IMAGE):rc-$(RC_SHA) (linux/amd64,linux/arm64)..."
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE):rc-$(RC_SHA) --push .
	@echo "✓ Pushed: $(IMAGE):rc-$(RC_SHA)"
	@echo "  Next: make uat-up REF=rc-$(RC_SHA)"

# ── Pre-promotion UAT (validate a pulled image before tagging/deploying) ──

# uat-up / uat-validate / uat-down: pull and run a specific published image
# (rc-<shortsha> or an already-promoted vX.Y.Z) locally, then run the same
# Hurl smoke+auth suite used for TrueNAS validation against it. --env-file is
# required here (not just env_file:) so ${RSS_CURATOR_IMAGE_REF}/${CURATOR_PASSWORD}
# compose-level interpolation is satisfied from uat.env.
uat-up:
	@if [ -z "$(REF)" ]; then \
		echo "Error: REF must be set, e.g. make uat-up REF=rc-abc1234"; \
		exit 1; \
	fi
	@if [ ! -f uat.env ]; then \
		echo "Error: uat.env not found"; \
		echo "Copy uat.env.sample to uat.env and configure it"; \
		exit 1; \
	fi
	RSS_CURATOR_IMAGE_REF=$(REF) $(CTR) compose --env-file uat.env -f docker-compose.uat.yml up -d
	@echo "✓ UAT stack running ($(REF)) — API at http://localhost:8081"

uat-down:
	$(CTR) compose --env-file uat.env -f docker-compose.uat.yml down --volumes

uat-logs:
	$(CTR) compose --env-file uat.env -f docker-compose.uat.yml logs -f

uat-validate:
	@mkdir -p tests/e2e/results
	$(CTR) compose --env-file uat.env -f docker-compose.uat.yml --profile validate run --rm hurl

# ── E2E / functional validation ──────────────────────────────────────────

# test-e2e: spin up a fresh curator container + Hurl sidecar, run smoke suite,
# tear everything down. Fails loudly if any Hurl assertion fails.
test-e2e:
	@mkdir -p tests/e2e/results
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from hurl
	docker compose -f docker-compose.test.yml down --volumes

# validate-smoke: run smoke + auth tests against the LIVE stack on TrueNAS
# (or any already-running curator instance). Requires CURATOR_USERNAME and
# CURATOR_PASSWORD to be exported in the calling shell.
#
# Example:
#   export CURATOR_USERNAME=admin CURATOR_PASSWORD=secret
#   make validate-smoke
validate-smoke:
	@if [ -z "$${CURATOR_USERNAME}" ] || [ -z "$${CURATOR_PASSWORD}" ]; then \
		echo "Error: CURATOR_USERNAME and CURATOR_PASSWORD must be exported"; \
		exit 1; \
	fi
	@mkdir -p tests/e2e/results
	docker compose -f docker-compose.validate.yml up --abort-on-container-exit --exit-code-from hurl
	docker compose -f docker-compose.validate.yml down