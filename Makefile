.PHONY: all build build-frontend build-backend build-dev build-backend-dev clean run run-dev kill-port kill-dev-port dev install frontend backend test lint help version set-version release release-binary extract-changelog

# Variables
BINARY_NAME=axonrouter
DEV_BINARY_NAME=axonrouter-dev
BUILD_DIR=./build
FRONTEND_DIR=./web
GO_BUILD_FLAGS=-ldflags="-s -w"
GO ?= /usr/local/go/bin/go
PORT=3777
DEV_PORT ?= 3788
VERSION_FILE=internal/version/VERSION
VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo unknown)
VERSION_TAG=v$(VERSION)
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

# Default target
all: build

# Build everything
build: build-frontend build-backend

# Build frontend
build-frontend:
	@echo "Building frontend..."
	cd $(FRONTEND_DIR) && npm run build
	@echo "Frontend built successfully!"

# Build backend (requires Go)
build-backend:
	@echo "Building backend..."
	@echo "Version: $(VERSION)"
	$(GO) build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server
	@echo "Backend built successfully!"
# Build dev backend (separate axonrouter-dev binary; never clobbers the live axonrouter)
build-backend-dev:
	@echo "Building dev backend..."
	$(GO) build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(DEV_BINARY_NAME) ./cmd/server
	@echo "Dev backend built successfully!"

# Build everything for dev (frontend + separate dev binary)
build-dev: build-frontend build-backend-dev

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -rf $(FRONTEND_DIR)/build
	rm -rf $(FRONTEND_DIR)/.svelte-kit
	# Also remove stray binaries, DB files, and session logs left in the workspace
	rm -f server axonrouter v1.test
	rm -rf bin/
	rm -f *.db *.db-wal *.db-shm
	rm -f console-*.log
	@echo "Cleaned!"

# Kill process listening on a port. Use LISTEN filter so outbound connections
# (e.g. OMP tunnels to a remote :3777) are not killed.
kill-port:
	@echo "Killing process listening on port $(PORT)..."
	@lsof -ti TCP:$(PORT) -sTCP:LISTEN | xargs kill -9 2>/dev/null || true
	@sleep 0.5
	@echo "Port $(PORT) cleared."

# Kill process listening on dev port.
kill-dev-port:
	@echo "Killing process listening on port $(DEV_PORT)..."
	@lsof -ti TCP:$(DEV_PORT) -sTCP:LISTEN | xargs kill -9 2>/dev/null || true
	@sleep 0.5
	@echo "Port $(DEV_PORT) cleared."

# Run the server
run: build kill-port
	@echo "Starting server on port $(PORT)..."
	$(BUILD_DIR)/$(BINARY_NAME)

# Run a dev server on an alternate port with isolated data so the main gateway keeps running.
# Uses a SEPARATE axonrouter-dev binary so rebuilding dev never touches the live gateway.
run-dev: build-dev kill-dev-port
	@mkdir -p /tmp/axon-dev
	@echo "Starting dev server on port $(DEV_PORT)..."
	@echo "Dev binary: $(BUILD_DIR)/$(DEV_BINARY_NAME)"
	@echo "Data directory: /tmp/axon-dev/axonrouter (main gateway on port $(PORT) is untouched)."
	HOME=/tmp/axon-dev AXON_PORT=$(DEV_PORT) $(BUILD_DIR)/$(DEV_BINARY_NAME)

# Development mode (frontend only)
dev:
	@echo "Starting frontend development server..."
	cd $(FRONTEND_DIR) && npm run dev

# Install frontend dependencies
install:
	@echo "Installing frontend dependencies..."
	cd $(FRONTEND_DIR) && npm install
	@echo "Dependencies installed!"

# Build frontend only
frontend: build-frontend

# Build backend only
backend: build-backend

# Run backend tests
test:
	@echo "Running Go tests..."
	$(GO) test ./...

# Run static analysis (go vet)
lint:
	@echo "Running go vet..."
	$(GO) vet ./...

# Show help
help:
	@echo "Available targets:"
	@echo " version           - Show current version from $(VERSION_FILE)"
	@echo " set-version       - Bump version and sync derived files (usage: make set-version v=0.3.1)"
	@echo " extract-changelog - Extract changelog notes for the current VERSION"
	@echo " release           - Bump version, commit, tag, and push (usage: make release v=0.3.1)"
	@echo " release-binary    - Cross-compile a release binary for GOOS/GOARCH"
	@echo " all - Build everything (default)"
	@echo "  build         - Build frontend and backend"
	@echo "  build-frontend - Build frontend only"
	@echo "  build-backend - Build backend only"
	@echo "  clean         - Clean build artifacts"
	@echo " run-dev - Run dev server (separate $(DEV_BINARY_NAME) binary) on port $(DEV_PORT) (main port $(PORT) untouched)"
	@echo "  kill-port     - Kill process on port $(PORT)"
	@echo "  dev           - Start frontend development server"
	@echo "  install       - Install frontend dependencies"
	@echo " frontend - Build frontend only"
	@echo " backend - Build backend only"
	@echo " test - Run Go tests"
	@echo " lint - Run go vet"
	@echo " help - Show this help"

# Show current version
version:
	@echo $(VERSION)

# Bump version and sync derived files. Requires v=X.Y.Z.
set-version:
	@if [ -z "$(v)" ]; then \
		echo "Usage: make set-version v=0.3.1"; \
		exit 1; \
	fi
	@node scripts/bump-version.js $(v)

# Extract changelog notes for the current VERSION. Redirect to a file as needed.
extract-changelog:
	@node scripts/extract-changelog.js $(VERSION)

# Build a release binary for the configured GOOS/GOARCH.
release-binary:
	@mkdir -p $(BUILD_DIR)
	@if [ "$(GOOS)" = "windows" ]; then \
		GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH).exe ./cmd/server; \
	else \
		GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) ./cmd/server; \
	fi

# Bump version, commit, tag, and push. Requires v=X.Y.Z and a clean working tree.
release:
	@if [ -z "$(v)" ]; then \
		echo "Usage: make release v=0.3.1"; \
		exit 1; \
	fi
	@if ! git diff --cached --quiet || ! git diff --quiet; then \
		echo "Working tree is not clean. Commit or stash changes before release."; \
		exit 1; \
	fi
	@$(MAKE) set-version v=$(v)
	@git add -A
	@git commit -m "release: v$(v)"
	@git tag -a "v$(v)" -m "Release v$(v)"
	@git push origin main "v$(v)"
	@echo "Released v$(v)"
