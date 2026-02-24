.PHONY: build clean install test help docker-build docker-run docker-push

BINARY_NAME=curator
INSTALL_PATH=/usr/local/bin
DOCKER_IMAGE_NAME=rss-curator
DOCKER_REGISTRY=ghcr.io/iillmaticc
DOCKER_IMAGE=$(DOCKER_REGISTRY)/$(DOCKER_IMAGE_NAME)

help:
	@echo "RSS Curator - Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build         Build the binary"
	@echo "  make install       Build and install to $(INSTALL_PATH)"
	@echo "  make test          Run tests"
	@echo "  make clean         Remove built binary"
	@echo "  make run           Build and run (requires .curator.env)"
	@echo ""
	@echo "Docker targets:"
	@echo "  make docker-build  Build Docker image"
	@echo "  make docker-run    Run Docker container (requires .env)"
	@echo "  make docker-push   Push image to registry (requires auth)"
	@echo "  make docker-clean  Remove Docker image"

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
docker-build:
	@echo "Building Docker image: $(DOCKER_IMAGE):latest"
	docker build -t $(DOCKER_IMAGE):latest .
	@echo "✓ Built Docker image: $(DOCKER_IMAGE):latest"

docker-run: docker-build
	@if [ -f .env ]; then \
		echo "Running Docker container with .env configuration"; \
		docker run --rm --env-file .env --network host -v curator-data:/app/data $(DOCKER_IMAGE):latest check; \
	else \
		echo "Error: .env file not found"; \
		echo "Copy curator.env.sample to .env and configure it"; \
		exit 1; \
	fi

docker-push:
	@echo "Pushing Docker image to registry: $(DOCKER_IMAGE)"
	docker push $(DOCKER_IMAGE):latest
	@echo "✓ Pushed Docker image"

docker-clean:
	@echo "Removing Docker image: $(DOCKER_IMAGE):latest"
	docker rmi $(DOCKER_IMAGE):latest
	@echo "✓ Removed Docker image"