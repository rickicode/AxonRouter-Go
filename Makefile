.PHONY: all build build-frontend build-backend clean run kill-port dev

# Variables
BINARY_NAME=axonrouter
BUILD_DIR=./build
FRONTEND_DIR=./web
GO_BUILD_FLAGS=-ldflags="-s -w"
GO=/usr/local/go/bin/go
PORT=3777

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

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -rf $(FRONTEND_DIR)/build
	rm -rf $(FRONTEND_DIR)/.svelte-kit
	@echo "Cleaned!"

# Kill process on port
kill-port:
	@echo "Killing process on port $(PORT)..."
	@lsof -ti :$(PORT) | xargs kill -9 2>/dev/null || true
	@sleep 0.5
	@echo "Port $(PORT) cleared."

# Run the server
run: build kill-port
	@echo "Starting server on port $(PORT)..."
	$(BUILD_DIR)/$(BINARY_NAME)

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
	@echo "  run           - Kill port, build, and run the server"
	@echo "  kill-port     - Kill process on port $(PORT)"
	@echo "  dev           - Start frontend development server"
	@echo "  install       - Install frontend dependencies"
	@echo "  frontend      - Build frontend only"
	@echo "  backend       - Build backend only"
	@echo "  help          - Show this help"
