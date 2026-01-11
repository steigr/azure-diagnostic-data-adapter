# Architecture

## Overview

The Azure Diagnostic Data Adapter (adda) is designed as a pipeline-based data processor that reads blobs from Azure Storage, parses them, enriches the data, and writes to NDJSON files.

```
┌─────────────────┐     ┌──────────┐     ┌──────────┐     ┌──────────┐
│  Azure Storage  │────▶│  Parser  │────▶│ Enricher │────▶│  Writer  │
│  (blob reader)  │     │          │     │          │     │ (NDJSON) │
└─────────────────┘     └──────────┘     └──────────┘     └──────────┘
        │                    │                │                │
        │                    │                │                │
        ▼                    ▼                ▼                ▼
   ┌─────────────────────────────────────────────────────────────┐
   │                     Metrics (Prometheus)                     │
   └─────────────────────────────────────────────────────────────┘
```

## Components

### Reader (`pkg/reader`)

Handles Azure Blob Storage operations:
- Authentication via `DefaultAzureCredential` or connection string
- Listing blobs with regex pattern filtering (case-insensitive)
- Sorting blobs by last modified time (oldest/newest first)
- Age-based filtering (min-age and max-age)
- Batch limiting for controlled processing
- Downloading blob content
- Deleting processed blobs
- Hierarchical path support for blob names

**Key Features:**
- `ListWithOptions()` supports sorting, limiting, and age filtering
- Blobs contain metadata: Name, Size, ContentType, LastModified

### Parser (`pkg/parser`)

Parses blob content into structured records:

#### NDJSON Parser (`pkg/parser/ndjson`) - Default
- Handles Newline Delimited JSON (one JSON object per line)
- This is the default parser for `.json` files
- Skips invalid lines and logs errors for debugging
- Most common format for log data and streaming JSON
- Case-insensitive file pattern matching

#### JSON Parser (`pkg/parser/json`)
- Handles JSON arrays and single JSON objects
- Use for structured JSON data (not line-delimited)
- Returns all elements of arrays as individual records

#### External Parser (`pkg/parser/external`)
- Runs external commands for custom parsing
- **File mode:** Uses INPUT_FILE and OUTPUT_FILE environment variables
- **Stdin/Stdout mode:** Pipes data through the command
- Supports command arguments and custom environment
- Expects NDJSON output format
- Each parsing operation gets a unique temporary directory

### Enricher (`pkg/enricher`)

Adds metadata to parsed records using Go templates:
- Source blob information (name, container, storage account)
- Processing timestamp and last modified time
- Blob size and content type
- **Hierarchical path parsing:** Directory, FileName, Extension, PathParts
- Custom fields via templates
- Built-in template functions for path manipulation

**Available Metadata:**
```go
type Metadata struct {
    BlobName       string
    ContainerName  string
    StorageAccount string
    ProcessedAt    time.Time
    Size           int64
    ContentType    string
    LastModified   time.Time
    // Hierarchical path components
    Directory      string    // Parent directory
    FileName       string    // Base filename
    Extension      string    // File extension (no dot)
    PathParts      []string  // All path components
}
```

### Writer (`pkg/writer`)

Writes enriched records to NDJSON files:
- Log rotation (size-based and time-based)
- Optional gzip compression
- First-write padding (1025 bytes minimum)
- Thread-safe writing

### Metrics (`pkg/metrics`)

Exposes Prometheus metrics:
- Processing counters and histograms
- Disk space monitoring
- Active parser tracking

### Processor (`cmd/adda/processor`)

Orchestrates the pipeline:
- Manages concurrency with worker pools
- Implements backoff based on disk space
- Handles retries and error recovery
- Supports dry-run mode for previewing
- Supports once mode with configurable limits

## Configuration

Configuration is loaded from multiple sources (in order of precedence):
1. Command-line flags
2. Environment variables (prefixed with `ADDA_`)
3. Configuration file (YAML)

See [CONFIGURATION.md](CONFIGURATION.md) for complete reference.

## Data Flow

1. **List**: Reader lists blobs matching the configured pattern
   - Applies file pattern filter (regex, case-insensitive)
   - Applies age filters (min-age, max-age)
   - Sorts by last modified time
   - Applies batch limit
2. **Download**: Each blob is downloaded to memory
3. **Parse**: Content is parsed by the matching parser
   - Parsers are matched by file pattern in order
4. **Enrich**: Records are enriched with metadata
   - Go template produces JSON merged with record
5. **Write**: Enriched records are written as NDJSON
   - First write padded to 1025 bytes (unless gzip)
6. **Delete**: Successfully processed blobs are deleted (if configured)

## Concurrency

- Worker pool for parallel blob processing
- Configurable worker count
- Semaphore-based concurrency control
- Thread-safe metrics and writing

## Error Handling

- Retry logic for transient failures
- Per-blob error isolation
- Per-line error handling in NDJSON parser
- Metrics for failed operations
- Graceful shutdown on signals

## Operation Modes

### Continuous Mode (Default)
Runs continuously, processing blobs as they appear.

### Once Mode (`--once`)
Processes available blobs once and exits.
- Configurable limit via `--once-limit` (default: 10)

### Dry Run Mode (`--dry-run`)
Previews what would be processed without making changes.
- Lists matched blobs
- Shows parser assignments
- Displays enrichment preview

## Filtering Options

### File Pattern
Regex pattern to match blob names (case-insensitive).

### Age Filtering
- `min-age`: Skip blobs newer than specified age
- `max-age`: Skip blobs older than specified age
- Useful for avoiding partially written files or old data

### Batch Limiting
- `batch-limit`: Max blobs per processing cycle
- `once-limit`: Max blobs for once mode


