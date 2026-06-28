.PHONY: all build build-frontend build-backend clean run dev

# Variables
BINARY_NAME=axonrouter
BUILD_DIR=./build
FRONTEND_DIR=./web
GO_BUILD_FLAGS=-ldflags="-s -w"

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
	go build $(GO_BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/server
	@echo "Backend built successfully!"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -rf $(FRONTEND_DIR)/build
	rm -rf $(FRONTEND_DIR)/.svelte-kit
	@echo "Cleaned!"

# Run the server
run: build
	@echo "Starting server..."
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
	@echo "  run           - Build and run the server"
	@echo "  dev           - Start frontend development server"
	@echo "  install       - Install frontend dependencies"
	@echo "  frontend      - Build frontend only"
	@echo "  backend       - Build backend only"
	@echo "  help          - Show this help"
