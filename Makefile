.PHONY: build clean install test help run \
        dev-up dev-down dev-logs dev-rebuild dev-clean \
        image-build image-push image-clean \
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