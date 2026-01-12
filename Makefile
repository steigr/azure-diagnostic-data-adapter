# Azure Diagnostic Data Adapter Makefile

BINARY_NAME=adda
VERSION?=git-$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags for optimized binary size
# -s: Omit symbol table and debug information
# -w: Omit DWARF debugging information
# -X: Embed version information
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Debug build flags (includes debug info)
LDFLAGS_DEBUG=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.gitCommit=$(GIT_COMMIT)"

# Container settings
CONTAINER_TOOL?=docker
IMAGE_NAME?=steigr/azure-diagnostic-data-adapter

# Azurite settings
AZURITE_DATA_DIR?=/tmp/azurite
AZURITE_PID_FILE?=/tmp/azurite.pid

# Tool versions
GOLANGCI_LINT_VERSION?=v2.8.0

.PHONY: all build build-debug clean test test-unit test-e2e test-e2e-ci lint fmt vet run docker docker-push help
.PHONY: azurite-install azurite-start azurite-stop azurite-check
.PHONY: tools tools-golangci-lint tools-azurite
.PHONY: test-manual test-manual-min-age test-manual-disk-space

all: clean lint test build ## Clean, lint, test, and build

build: ## Build the binary (optimized, stripped)
	@echo "Building $(BINARY_NAME) (optimized)..."
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/adda

build-debug: ## Build the binary with debug info
	@echo "Building $(BINARY_NAME) (debug)..."
	go build $(LDFLAGS_DEBUG) -o bin/$(BINARY_NAME) ./cmd/adda

build-linux: ## Build for Linux (optimized, stripped)
	@echo "Building $(BINARY_NAME) for Linux (optimized)..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/adda

build-linux-debug: ## Build for Linux with debug info
	@echo "Building $(BINARY_NAME) for Linux (debug)..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS_DEBUG) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/adda

build-all: build build-linux ## Build for all platforms (optimized)

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf bin/
	go clean

test: test-unit ## Run unit tests (alias for test-unit)

test-unit: ## Run unit tests (excluding e2e)
	@echo "Running unit tests..."
	go test -v -race -coverprofile=coverage.out $$(go list ./... | grep -v /tests/e2e)

test-short: ## Run short tests
	@echo "Running short tests..."
	go test -v -short ./...

test-e2e: ## Run e2e tests (requires Azurite running)
	@echo "Running e2e tests..."
	go test -v -race ./tests/e2e/...

test-e2e-ci: tools-azurite ## Run e2e tests in CI (installs and starts Azurite)
	@$(MAKE) azurite-start
	@echo "Running e2e tests..."
	@trap '$(MAKE) azurite-stop' EXIT; \
	sleep 2 && go test -v -race ./tests/e2e/...

test-all: test-unit test-e2e ## Run all tests (unit + e2e)

test-manual: build tools-azurite ## Run manual interactive test with Azurite
	@echo "Starting manual test environment..."
	@./scripts/test/manual-test.sh

test-manual-min-age: build tools-azurite ## Run min-age test with Azurite
	@echo "Starting min-age test..."
	@MIN_AGE_TEST=true MIN_AGE_SECONDS=15 ./scripts/test/manual-test.sh

test-manual-disk-space: build tools-azurite ## Run disk space backoff test with Azurite
	@echo "Starting disk space backoff test..."
	@DISK_SPACE_TEST=true ./scripts/test/manual-test.sh

coverage: test-unit ## Show test coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Tool installation targets
tools: tools-golangci-lint tools-azurite ## Install all development tools

tools-golangci-lint: ## Install golangci-lint
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); \
	else \
		echo "golangci-lint is already installed"; \
	fi

tools-azurite: ## Install Azurite (requires npm)
	@if ! command -v azurite-blob >/dev/null 2>&1; then \
		echo "Installing Azurite..."; \
		if command -v npm >/dev/null 2>&1; then \
			npm install -g azurite; \
		else \
			echo "Error: npm is required to install Azurite"; \
			echo "Please install Node.js and npm first"; \
			exit 1; \
		fi \
	else \
		echo "Azurite is already installed"; \
	fi

# Azurite targets (kept for backwards compatibility)
azurite-install: tools-azurite ## Install Azurite (alias for tools-azurite)

azurite-start: ## Start Azurite Blob Service in the background
	@echo "Starting Azurite Blob Service..."
	@mkdir -p $(AZURITE_DATA_DIR)
	@if [ -f $(AZURITE_PID_FILE) ] && kill -0 $$(cat $(AZURITE_PID_FILE)) 2>/dev/null; then \
		echo "Azurite Blob Service is already running (PID: $$(cat $(AZURITE_PID_FILE)))"; \
	else \
		azurite-blob --silent --location $(AZURITE_DATA_DIR) --debug $(AZURITE_DATA_DIR)/debug.log & \
		echo $$! > $(AZURITE_PID_FILE); \
		echo "Azurite Blob Service started (PID: $$(cat $(AZURITE_PID_FILE)))"; \
		sleep 2; \
	fi

azurite-stop: ## Stop Azurite Blob Service
	@echo "Stopping Azurite Blob Service..."
	@if [ -f $(AZURITE_PID_FILE) ]; then \
		if kill -0 $$(cat $(AZURITE_PID_FILE)) 2>/dev/null; then \
			kill $$(cat $(AZURITE_PID_FILE)) 2>/dev/null || true; \
			echo "Azurite Blob Service stopped"; \
		else \
			echo "Azurite Blob Service process not found"; \
		fi; \
		rm -f $(AZURITE_PID_FILE); \
	else \
		echo "No Azurite PID file found"; \
	fi

azurite-check: ## Check if Azurite Blob Service is running
	@if ! curl -s http://127.0.0.1:10000 >/dev/null 2>&1; then \
		echo "Warning: Azurite Blob Service does not appear to be running on port 10000"; \
		echo "Start it with: make azurite-start"; \
		echo "Or run: make test-e2e-ci (will install and start Azurite automatically)"; \
		echo ""; \
		echo "Proceeding with tests (they will be skipped if Azurite is not available)..."; \
	fi

lint: tools-golangci-lint ## Run linter (installs golangci-lint if needed)
	@echo "Running linter..."
	golangci-lint run ./...

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

run: build ## Build and run the binary
	@echo "Running $(BINARY_NAME)..."
	./bin/$(BINARY_NAME)

run-dry: build ## Run in dry-run mode
	./bin/$(BINARY_NAME) --dry-run

docker: ## Build Docker image
	@echo "Building Docker image..."
	$(CONTAINER_TOOL) buildx build --tag=$(IMAGE_NAME):$(VERSION) --load .

docker-push: docker ## Push Docker image
	@echo "Pushing Docker image..."
	$(CONTAINER_TOOL) push $(IMAGE_NAME):$(VERSION)

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download

deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	go get -u ./...
	go mod tidy

install: build ## Install the binary
	@echo "Installing $(BINARY_NAME)..."
	cp bin/$(BINARY_NAME) $(GOPATH)/bin/

help: ## Show this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*##.*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
