.PHONY: build build-server build-cli build-tui build-controlplane clean test test-verbose test-coverage test-cli test-integration fmt help

# Variables
BINARY_DIR = ./bin
SERVER_BIN = $(BINARY_DIR)/razpravljalnica-server
CLI_BIN = $(BINARY_DIR)/razpravljalnica-cli
TUI_BIN = $(BINARY_DIR)/razpravljalnica-tui
CONTROLPLANE_BIN = $(BINARY_DIR)/razpravljalnica-controlplane

# Build all binaries
build: build-server build-cli build-tui build-controlplane
	@echo "✓ All binaries built"

# Build server
build-server:
	@mkdir -p $(BINARY_DIR)
	@echo "Building server..."
	@go build -o $(SERVER_BIN) ./cmd/server

# Build CLI client
build-cli:
	@mkdir -p $(BINARY_DIR)
	@echo "Building CLI client..."
	@go build -o $(CLI_BIN) ./cmd/cli

# Build TUI client
build-tui:
	@mkdir -p $(BINARY_DIR)
	@echo "Building TUI client..."
	@go build -o $(TUI_BIN) ./cmd/tui

# Build control plane
build-controlplane:
	@mkdir -p $(BINARY_DIR)
	@echo "Building control plane..."
	@go build -o $(CONTROLPLANE_BIN) ./cmd/controlplane

# Run tests
test:
	@echo "Running tests..."
	@go test ./...

# Run tests with verbose output
test-verbose:
	@go test -v -race ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run CLI tests
test-cli: build
	@echo "Running CLI tests..."
	@go test -v ./cmd/cli/...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./tests/...

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BINARY_DIR)
	@rm -f coverage.out coverage.html
	@echo "✓ Clean complete"

# Development: build all and run server
dev-server: build-server
	@$(SERVER_BIN) -p 9876

# Development: build and run CLI
dev-cli: build-cli
	@$(CLI_BIN) -s localhost -p 9876

# Development: build and run TUI
dev-tui: build-tui
	@$(TUI_BIN) -s localhost -p 9876

# Format code
fmt:
	@go fmt ./...

# Install dependencies
deps:
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/razpravljalnica.proto

help:
	@echo "Available targets:"
	@echo "  make build              - Build all binaries"
	@echo "  make build-server       - Build server only"
	@echo "  make build-cli          - Build CLI client only"
	@echo "  make build-tui          - Build TUI client only"
	@echo "  make test               - Run all tests"
	@echo "  make test-verbose       - Run tests with race detector"
	@echo "  make test-coverage      - Run tests and generate coverage report"
	@echo "  make test-cli           - Run CLI tests"
	@echo "  make test-integration   - Run integration tests"
	@echo "  make clean              - Remove build artifacts"
	@echo "  make dev-server         - Build and run server"
	@echo "  make dev-cli            - Build and run CLI client"
	@echo "  make dev-tui            - Build and run TUI client"
	@echo "  make fmt                - Format code"
	@echo "  make deps               - Install dependencies"
	@echo "  make proto              - Regenerate protobuf code"
