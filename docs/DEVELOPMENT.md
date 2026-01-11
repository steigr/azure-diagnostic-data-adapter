# Development Guide

This document provides information for developers working on the Azure Diagnostic Data Adapter (adda).

## Prerequisites

- Go 1.21 or later
- Node.js and npm (for Azurite)
- Azure CLI (for testing with real Azure Storage)
- Docker (optional, for container builds)

## Getting Started

```bash
# Clone the repository
git clone https://github.com/steigr/azure-diagnostic-data-adapter.git
cd azure-diagnostic-data-adapter

# Install development tools
make tools

# Build the binary
make build

# Run tests
make test-unit
```

## Project Structure

```
.
├── bin/                    # Compiled binaries
├── cmd/
│   └── adda/
│       ├── main.go         # Entry point
│       ├── config/         # Configuration handling
│       └── processor/      # Main processing pipeline
├── docs/                   # Documentation
├── examples/               # Example configurations
├── internal/
│   └── utils/              # Internal utilities
├── pkg/
│   ├── enricher/           # Record enrichment
│   ├── metrics/            # Prometheus metrics
│   ├── parser/             # Parser interface and implementations
│   │   ├── external/       # External command parser
│   │   ├── json/           # JSON array parser
│   │   └── ndjson/         # NDJSON parser
│   ├── reader/             # Azure Blob Storage reader
│   └── writer/             # NDJSON file writer
├── scripts/
│   ├── parsers/            # Example external parsers
│   └── test/               # Test scripts
└── tests/
    └── e2e/                # End-to-end tests
```

## Make Targets

```bash
make help              # Show all available targets

# Building
make build             # Build optimized binary
make build-debug       # Build with debug symbols
make build-linux       # Cross-compile for Linux

# Testing
make test-unit         # Run unit tests
make test-e2e          # Run e2e tests (requires Azurite)
make test-e2e-ci       # Run e2e tests (starts Azurite automatically)
make test-manual       # Run interactive manual test
make test-manual-min-age  # Run min-age filter test

# Code Quality
make lint              # Run golangci-lint
make fmt               # Format code
make vet               # Run go vet

# Tools
make tools             # Install all dev tools
make tools-golangci-lint  # Install linter
make tools-azurite     # Install Azurite

# Azurite
make azurite-start     # Start Azurite blob service
make azurite-stop      # Stop Azurite blob service

# Other
make coverage          # Generate coverage report
make docker            # Build Docker image
make clean             # Clean build artifacts
```

## Testing

### Unit Tests

```bash
make test-unit
```

Unit tests cover individual components and don't require external services.

### End-to-End Tests

E2E tests use Azurite (Azure Storage Emulator) to test the full pipeline:

```bash
# Install and start Azurite
make tools-azurite
make azurite-start

# Run e2e tests
make test-e2e

# Stop Azurite
make azurite-stop
```

Or run everything automatically:

```bash
make test-e2e-ci
```

### Manual Testing

Interactive testing with Azurite:

```bash
make test-manual
```

This starts Azurite, uploads test data, and runs adda in debug mode. Press Ctrl+C to stop.

### Testing Min-Age Filter

```bash
make test-manual-min-age
```

This tests the blob age filtering feature by:
1. Uploading files
2. Running adda with `--blob-min-age 15s` (files should NOT be processed)
3. Waiting 15 seconds
4. Running adda again (files should be processed)

## Adding a New Parser

1. Create a new package in `pkg/parser/`:

```go
// pkg/parser/myformat/myformat.go
package myformat

import (
    "io"
    "github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
)

type Parser struct {
    *parser.BaseParser
}

func New(id, filePattern string) (*Parser, error) {
    base, err := parser.NewBaseParser(id, filePattern)
    if err != nil {
        return nil, err
    }
    return &Parser{BaseParser: base}, nil
}

func (p *Parser) Parse(input io.Reader) ([]map[string]any, error) {
    // Implementation
}
```

2. Register in `cmd/adda/processor/processor.go`:

```go
case "myformat":
    p, err = myformat.New(pcfg.ID, pcfg.FilePattern)
```

3. Update config validation in `cmd/adda/config/config.go`

4. Add tests and documentation

## Adding a New CLI Flag

1. Add the flag in `cmd/adda/main.go`:

```go
rootCmd.Flags().String("my-flag", "default", "Description")
```

2. Bind to viper:

```go
_ = viper.BindPFlag("category.my_flag", rootCmd.Flags().Lookup("my-flag"))
```

3. Add to config struct in `cmd/adda/config/config.go`

4. Add validation if needed

5. Update documentation

## Code Style

- Follow Go conventions
- Use `golangci-lint` for linting
- Write table-driven tests
- Document exported functions
- Handle errors explicitly

## Debugging

### Enable Debug Logging

```bash
./bin/adda --log-level debug --log-format text ...
```

### Dry Run Mode

Preview what would be processed without making changes:

```bash
./bin/adda --config config.yaml --dry-run
```

### Local Development with Azurite

```bash
# Start Azurite
make azurite-start

# Use connection string
./bin/adda \
  --connection-string "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;" \
  --container test \
  --log-level debug \
  --once
```

## Release Process

1. Update version in code (if applicable)
2. Update CHANGELOG
3. Run all tests: `make test-unit test-e2e-ci`
4. Build: `make build-all`
5. Tag release: `git tag v1.x.x`
6. Push: `git push origin v1.x.x`

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes with tests
4. Run `make lint test-unit`
5. Submit a pull request

## Troubleshooting Development

### Azurite Won't Start

```bash
# Check if port 10000 is in use
lsof -i :10000

# Kill existing process
make azurite-stop

# Remove data and restart
rm -rf /tmp/azurite
make azurite-start
```

### Build Errors

```bash
# Clean and rebuild
make clean
go mod tidy
make build
```

### Test Failures

```bash
# Run specific test with verbose output
go test -v -run TestName ./pkg/...

# Run with race detection
go test -race ./...
```

