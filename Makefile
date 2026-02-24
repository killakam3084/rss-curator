.PHONY: build clean install test help

BINARY_NAME=curator
INSTALL_PATH=/usr/local/bin

help:
	@echo "RSS Curator - Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build      Build the binary"
	@echo "  make install    Build and install to $(INSTALL_PATH)"
	@echo "  make test       Run tests"
	@echo "  make clean      Remove built binary"
	@echo "  make run        Build and run (requires .curator.env)"

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
