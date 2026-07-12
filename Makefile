.PHONY: all build build-frontend build-backend build-dev build-backend-dev clean run kill-port dev

# Variables
BINARY_NAME=axonrouter
DEV_BINARY_NAME=axonrouter-dev
BUILD_DIR=./build
FRONTEND_DIR=./web
GO_BUILD_FLAGS=-ldflags="-s -w"
GO=/usr/local/go/bin/go
PORT=3777
DEV_PORT ?= 3788

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
	@echo "Data directory: /tmp/axon-dev (main gateway on port $(PORT) is untouched)."
	AXON_DATA_DIR=/tmp/axon-dev AXON_PORT=$(DEV_PORT) $(BUILD_DIR)/$(DEV_BINARY_NAME)

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

# Show help
help:
	@echo "Available targets:"
	@echo "  all           - Build everything (default)"
	@echo "  build         - Build frontend and backend"
	@echo "  build-frontend - Build frontend only"
	@echo "  build-backend - Build backend only"
	@echo "  clean         - Clean build artifacts"
	@echo " run-dev - Run dev server (separate $(DEV_BINARY_NAME) binary) on port $(DEV_PORT) (main port $(PORT) untouched)"
	@echo "  kill-port     - Kill process on port $(PORT)"
	@echo "  dev           - Start frontend development server"
	@echo "  install       - Install frontend dependencies"
	@echo "  frontend      - Build frontend only"
	@echo "  backend       - Build backend only"
	@echo "  help          - Show this help"
