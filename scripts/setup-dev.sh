#!/bin/bash
# Development setup script for Azure Diagnostic Data Reader

set -e

echo "Setting up development environment..."

# Check Go version
GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | head -1)
echo "Go version: $GO_VERSION"

# Download dependencies
echo "Downloading dependencies..."
go mod download

# Install development tools
echo "Installing development tools..."

# golangci-lint for linting
if ! command -v golangci-lint &> /dev/null; then
    echo "Installing golangci-lint..."
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
fi

# Build the project
echo "Building project..."
go build -o bin/adda ./cmd/adda

# Run tests
echo "Running tests..."
go test -v ./...

echo ""
echo "Development environment setup complete!"
echo ""
echo "Available commands:"
echo "  make build    - Build the binary"
echo "  make test     - Run tests"
echo "  make lint     - Run linter"
echo "  make run      - Build and run"
echo "  make help     - Show all available targets"
