# Azure Diagnostic Data Adapter (adda)

A tool to read data from Azure Storage Account blob containers, parse, enrich, and write to NDJSON files.

## Features

- **Azure Blob Storage**: Read blobs from Azure Storage Account containers
- **Pattern Matching**: Filter blobs using regex patterns
- **Flexible Parsing**: Built-in JSON parser and support for external command parsers
- **Data Enrichment**: Add metadata using Go templates
- **NDJSON Output**: Write to newline-delimited JSON files with log rotation
- **Prometheus Metrics**: Monitor processing with built-in metrics
- **Backoff Support**: Back off when disk space is low
- **Dry Run Mode**: Test configuration without making changes

## Installation

### From Source

```bash
git clone https://github.com/steigr/azure-diagnostic-data-adapter.git
cd azure-diagnostic-data-adapter
make build
```

### Docker

```bash
docker pull ghcr.io/steigr/azure-diagnostic-data-adapter:latest
```

## Authentication

The tool supports multiple Azure authentication methods via `DefaultAzureCredential`:

1. **Environment Variables** (Service Principal):
   - `AZURE_TENANT_ID`
   - `AZURE_CLIENT_ID`
   - `AZURE_CLIENT_SECRET`

2. **Workload Identity** (Kubernetes):
   - `AZURE_FEDERATED_TOKEN_FILE`
   - `AZURE_AUTHORITY_HOST`

3. **Managed Identity** (Azure VMs, App Service)

4. **Azure CLI** (Development)

Alternatively, use a connection string:
```bash
adda --connection-string "DefaultEndpointsProtocol=https;AccountName=...;AccountKey=..."
```

## Usage

### Command Line

```bash
# Basic usage
adda --storage-account mystorageaccount --container diagnostics --output-dir /var/log/adda

# With config file
adda --config /etc/adda/config.yaml

# Dry run mode - shows what would be processed without making changes
adda --config config.yaml --dry-run

# Process once and exit
adda --config config.yaml --once

# With custom file pattern
adda --storage-account mystorageaccount --container logs --file-pattern '.*\.json$'
```

### Dry Run Mode

The `--dry-run` flag runs the tool in preview mode (implies `--once`). It will:
- List all blobs matching the configured pattern
- Show which parser would be used for each blob
- Display a preview of how the enrichment template transforms records
- Show the output configuration

This is useful for testing your configuration before running in production.

### Configuration File

Create a `config.yaml` file:

```yaml
source:
  storage_account_name: "mystorageaccount"
  container_name: "diagnostics"
  file_pattern: ".*\\.json$"

output:
  directory: "/var/log/adda"
  filename: "diagnostics.ndjson"
  max_size: 100  # MB
  max_backups: 5
  max_age: 30    # days
  gzip: false    # Enable gzip compression

parsers:
  - id: "ndjson"
    type: "ndjson"
    file_pattern: ".*\\.json$"

enrichment_template: |
  {
    "_source": {
      "blob": "{{ .Metadata.BlobName }}",
      "container": "{{ .Metadata.ContainerName }}",
      "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}"
    }
  }

metrics:
  enabled: true
  address: ":9090"

processing:
  workers: 2
  delete_after_process: true
  min_free_space_gb: 1

logging:
  level: "info"
  format: "json"
```

### Parsers

#### NDJSON Parser (Default)

The built-in NDJSON parser handles **newline-delimited JSON**, where each line contains a single JSON object. This is the most common format for log data and is the default parser for `.json` files.

Features:
- Parses one JSON object per line
- Skips empty lines and whitespace-only lines
- **Resilient parsing**: If a line fails to parse, the error is logged (with line number and content) and processing continues with the next line

Example NDJSON input:
```json
{"timestamp":"2024-01-01T00:00:00Z","message":"Event 1"}
{"timestamp":"2024-01-01T00:00:01Z","message":"Event 2"}
{"timestamp":"2024-01-01T00:00:02Z","message":"Event 3"}
```

Configuration:
```yaml
parsers:
  - id: "ndjson"
    type: "ndjson"
    file_pattern: ".*\\.json$"
```

#### JSON Parser

The JSON parser handles **JSON arrays** and **single JSON objects**. Use this when your files contain structured JSON rather than line-delimited data.

Features:
- Parses JSON arrays (returns each element as a record)
- Parses single JSON objects (returns one record)
- Does NOT support NDJSON format (use the ndjson parser for that)

Example JSON array input:
```json
[
  {"timestamp":"2024-01-01T00:00:00Z","message":"Event 1"},
  {"timestamp":"2024-01-01T00:00:01Z","message":"Event 2"}
]
```

Configuration:
```yaml
parsers:
  - id: "json-array"
    type: "json"
    file_pattern: ".*\\.json-array$"
```

#### External Parser

To use an external parser:

```yaml
parsers:
  - id: "custom"
    type: "external"
    file_pattern: ".*\\.csv$"
    command: "/usr/local/bin/my-parser"
    args: ["--format", "json"]
    env:
      PARSER_CONFIG: "/etc/my-parser.conf"
```

Each parser has a `file_pattern` (regex) that determines which blobs it will process. The first parser whose pattern matches the blob name will be used. If no pattern is specified, the parser matches all files.

The external command receives the following environment variables:
- `TEMP_DIR`: Path to the unique temporary directory for this parsing operation
- `INPUT_FILE`: Path to the input file (inside TEMP_DIR)
- `OUTPUT_FILE`: Path where JSON output should be written (inside TEMP_DIR)

The working directory is also set to `TEMP_DIR`. Each parsing operation gets its own unique temporary directory, which is automatically cleaned up after parsing completes.

You can configure the base temporary directory using `--temp-dir` flag or `processing.temp_dir` in the config file.

### Enrichment Template

The enrichment template is a Go text/template that produces valid JSON. Available variables:

- `.Record` - The parsed record (map)
- `.Metadata.BlobName` - Name of the source blob
- `.Metadata.ContainerName` - Azure container name
- `.Metadata.StorageAccount` - Azure storage account name
- `.Metadata.ProcessedAt` - Processing timestamp (time.Time)
- `.Metadata.Size` - Blob size in bytes
- `.Metadata.ContentType` - Blob content type

## Metrics

Prometheus metrics are exposed at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `adda_blobs_processed_total` | Counter | Total processed blobs |
| `adda_blobs_failed_total` | Counter | Total failed processing attempts |
| `adda_processed_bytes_total` | Counter | Total processed bytes |
| `adda_lines_written_total` | Counter | Total lines written |
| `adda_blob_processing_seconds` | Histogram | Processing time per blob |
| `adda_output_dir_free_bytes` | Gauge | Free space in output directory |
| `adda_active_parsers` | Gauge | Currently active parsers |

Health check endpoint: `/health`

## Docker Usage

```bash
docker run -v /path/to/config.yaml:/etc/adda/config.yaml \
           -v /var/log/adda:/var/log/adda \
           -e AZURE_TENANT_ID=... \
           -e AZURE_CLIENT_ID=... \
           -e AZURE_CLIENT_SECRET=... \
           -p 9090:9090 \
           ghcr.io/steigr/azure-diagnostic-data-adapter:latest
```

## Development

```bash
# Build
make build

# Run tests
make test

# Run linter
make lint

# Build Docker image
make docker
```

## License

MIT License
