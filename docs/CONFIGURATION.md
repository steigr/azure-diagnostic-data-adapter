# Configuration Reference

This document provides a complete reference for all configuration options available in the Azure Diagnostic Data Adapter (adda).

## Configuration Sources

Configuration is loaded from multiple sources in the following order of precedence (highest to lowest):

1. **Command-line flags** - Override all other sources
2. **Environment variables** - Prefixed with `ADDA_`
3. **Configuration file** - YAML format

## Configuration File Location

The configuration file is searched in the following locations:

1. Path specified by `--config` flag
2. `./config.yaml` (current directory)
3. `$HOME/.adda/config.yaml`
4. `/etc/adda/config.yaml`

## Complete Configuration Reference

### Source Configuration

```yaml
source:
  # Azure Storage Account name (uses DefaultAzureCredential for auth)
  storage_account_name: "mystorageaccount"
  
  # Alternatively, use a connection string
  connection_string: "DefaultEndpointsProtocol=https;AccountName=...;AccountKey=..."
  
  # Blob container name (required)
  container_name: "diagnostics"
  
  # Regex pattern to filter blobs (optional)
  file_pattern: ".*\\.json$"
```

| Field | CLI Flag | Environment Variable | Default | Description |
|-------|----------|---------------------|---------|-------------|
| `storage_account_name` | `--storage-account` | `ADDA_SOURCE_STORAGE_ACCOUNT_NAME` | - | Azure Storage Account name |
| `connection_string` | `--connection-string` | `ADDA_SOURCE_CONNECTION_STRING` | - | Azure Storage connection string |
| `container_name` | `--container` | `ADDA_SOURCE_CONTAINER_NAME` | - | Blob container name |
| `file_pattern` | `--file-pattern` | `ADDA_SOURCE_FILE_PATTERN` | - | Regex pattern for blob filtering |

### Output Configuration

```yaml
output:
  # Directory for output files
  directory: "/var/log/adda"
  
  # Output filename
  filename: "output.ndjson"
  
  # Max file size in MB before rotation
  max_size: 100
  
  # Max number of backup files
  max_backups: 3
  
  # Max age in days for backup files
  max_age: 28
  
  # Enable gzip compression for output
  gzip: false
```

| Field | CLI Flag | Environment Variable | Default | Description |
|-------|----------|---------------------|---------|-------------|
| `directory` | `--output-dir` | `ADDA_OUTPUT_DIRECTORY` | `.` | Output directory |
| `filename` | `--output-file` | `ADDA_OUTPUT_FILENAME` | `output.ndjson` | Output filename |
| `max_size` | `--max-size` | `ADDA_OUTPUT_MAX_SIZE` | `100` | Max file size in MB |
| `max_backups` | `--max-backups` | `ADDA_OUTPUT_MAX_BACKUPS` | `3` | Max backup files |
| `max_age` | `--max-age` | `ADDA_OUTPUT_MAX_AGE` | `28` | Max age in days |
| `gzip` | `--gzip` | `ADDA_OUTPUT_GZIP` | `false` | Enable gzip compression |

### Parser Configuration

```yaml
parsers:
  # NDJSON parser (default)
  - id: "ndjson"
    type: "ndjson"
    file_pattern: ".*\\.json$"
  
  # JSON array parser
  - id: "json-array"
    type: "json"
    file_pattern: ".*\\.json-array$"
  
  # External parser (file mode)
  - id: "csv"
    type: "external"
    file_pattern: ".*\\.csv$"
    command: "/usr/local/bin/csv-to-json"
    args: ["--format", "json"]
    env:
      PARSER_CONFIG: "/etc/parser.conf"
  
  # External parser (stdin/stdout mode)
  - id: "csv-stream"
    type: "external"
    file_pattern: ".*\\.tsv$"
    command: "csv-parser"
    args: ["--stdin", "--stdout"]
    stdin: true
    stdout: true
```

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the parser |
| `type` | string | Parser type: `ndjson`, `json`, or `external` |
| `file_pattern` | string | Regex pattern to match blob names (case-insensitive) |
| `command` | string | External command to run (for `external` type) |
| `args` | []string | Command arguments |
| `env` | map[string]string | Additional environment variables |
| `stdin` | bool | Read input from stdin (external parser) |
| `stdout` | bool | Write output to stdout (external parser) |

### Processing Configuration

```yaml
processing:
  # Number of parallel workers
  workers: 1
  
  # Dry run mode (no changes made)
  dry_run: false
  
  # Delete blobs after processing
  delete_after_process: true
  
  # Enable backoff on low disk space
  backoff_enabled: true
  
  # Minimum free space with unit (default: 256MiB)
  # Supported units: B, KB, KiB, MB, MiB, GB, GiB, TB, TiB
  min_free_space: "256MiB"
  
  # Retry attempts for failed operations
  retry_attempts: 3
  
  # Delay between retries
  retry_delay: "5s"
  
  # Temporary directory for external parsers
  temp_dir: "/tmp"
  
  # Sort order: "oldest" or "newest"
  sort_order: "oldest"
  
  # Max blobs per batch (0 = unlimited)
  batch_limit: 0
  
  # Max blobs for --once mode
  once_limit: 10
  
  # Minimum age of blobs to process (0 = no minimum)
  min_age: "0s"
  
  # Maximum age of blobs to process (0 = no maximum)
  max_age: "0s"
  
  # Interval between polling for new blobs (default: 1m)
  poll_interval: "1m"
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `workers` | `--workers` | `1` | Parallel workers |
| `dry_run` | `--dry-run` | `false` | Preview mode |
| `delete_after_process` | `--delete-after-process` | `true` | Delete processed blobs |
| `backoff_enabled` | - | `true` | Enable disk space backoff |
| `min_free_space` | `--min-free-space` | `256MiB` | Min free space with unit |
| `retry_attempts` | - | `3` | Retry count |
| `retry_delay` | - | `5s` | Retry delay |
| `temp_dir` | `--temp-dir` | System temp | Temp directory |
| `sort_order` | `--sort-order` | `oldest` | Blob sort order |
| `batch_limit` | `--batch-limit` | `0` | Max blobs per batch |
| `once_limit` | `--once-limit` | `10` | Max blobs for --once |
| `min_age` | `--blob-min-age` | `0s` | Min blob age |
| `max_age` | `--blob-max-age` | `0s` | Max blob age |
| `poll_interval` | `--poll-interval` | `1m` | Poll interval |

#### Disk Space Backoff

When `backoff_enabled` is true, the adapter monitors free disk space in the output directory:

- **Startup check**: Logs a warning if free space is below the threshold
- **Continuous mode**: Skips processing cycles until space is available
- **Once mode**: Returns an error if insufficient disk space

The minimum free space is configured using human-readable byte size notation:

**Supported units:**
- `B` - Bytes
- `KB` - Kilobytes (1000 bytes)
- `KiB` - Kibibytes (1024 bytes)
- `MB` - Megabytes (1000 KB)
- `MiB` - Mebibytes (1024 KiB)
- `GB` - Gigabytes (1000 MB)
- `GiB` - Gibibytes (1024 MiB)
- `TB` - Terabytes (1000 GB)
- `TiB` - Tebibytes (1024 GiB)

**Examples:**
```yaml
processing:
  min_free_space: "1GB"      # 1 gigabyte (1,000,000,000 bytes)
  min_free_space: "1GiB"     # 1 gibibyte (1,073,741,824 bytes)
  min_free_space: "500MB"    # 500 megabytes
  min_free_space: "256MiB"   # 256 mebibytes
```

**Prometheus metrics:**
- `adda_output_dir_free_bytes`: Current free space
- `adda_backoff_total`: Number of backoff events
- `adda_backoff_active`: Whether backoff is currently active (1) or not (0)

### Metrics Configuration

```yaml
metrics:
  # Enable Prometheus metrics
  enabled: true
  
  # Metrics server address
  address: ":9090"
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `enabled` | `--metrics` | `true` | Enable metrics |
| `address` | `--metrics-address` | `:9090` | Metrics address |

### Logging Configuration

```yaml
logging:
  # Log level: debug, info, warn, error
  level: "info"
  
  # Log format: text, json
  format: "text"
```

| Field | CLI Flag | Default | Description |
|-------|----------|---------|-------------|
| `level` | `--log-level` | `info` | Log level |
| `format` | `--log-format` | `text` | Log format |

### Enrichment Template

```yaml
enrichment_template: |
  {
    "_source": {
      "blob": "{{ .Metadata.BlobName }}",
      "container": "{{ .Metadata.ContainerName }}",
      "storage_account": "{{ .Metadata.StorageAccount }}",
      "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}",
      "last_modified": "{{ .Metadata.LastModified.Format "2006-01-02T15:04:05Z07:00" }}",
      "size": {{ .Metadata.Size }},
      "directory": "{{ .Metadata.Directory }}",
      "filename": "{{ .Metadata.FileName }}",
      "extension": "{{ .Metadata.Extension }}"
    }
  }
```

See [ENRICHMENT.md](ENRICHMENT.md) for detailed documentation on enrichment templates.

## Example Configurations

### Minimal Configuration

```yaml
source:
  connection_string: "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=..."
  container_name: "logs"

output:
  directory: "/var/log/adda"
```

### Production Configuration

```yaml
source:
  storage_account_name: "prodlogs"
  container_name: "diagnostics"
  file_pattern: ".*\\.(json|ndjson)$"

output:
  directory: "/var/log/adda"
  filename: "diagnostics.ndjson"
  max_size: 100
  max_backups: 10
  max_age: 30
  gzip: true

parsers:
  - id: "ndjson"
    type: "ndjson"
    file_pattern: ".*\\.json$"

enrichment_template: |
  {
    "_meta": {
      "source": "{{ .Metadata.BlobName }}",
      "ingested_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}"
    }
  }

metrics:
  enabled: true
  address: ":9090"

processing:
  workers: 4
  delete_after_process: true
  min_free_space_gb: 10
  sort_order: "oldest"
  min_age: "30s"

logging:
  level: "info"
  format: "json"
```

### Development Configuration

```yaml
source:
  connection_string: "UseDevelopmentStorage=true"
  container_name: "test"

output:
  directory: "./output"
  filename: "test.ndjson"

processing:
  delete_after_process: false
  dry_run: false

logging:
  level: "debug"
  format: "text"
```

