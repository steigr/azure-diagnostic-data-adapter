= Azure Diagnostic Data Adapter

A production-ready tool to read data from Azure Storage Account blob containers, parse, enrich, and write to NDJSON files.

== Status: ✅ IMPLEMENTED

All core features have been implemented and tested.

== Core Features

=== Data Pipeline ✅
* Read blobs from Azure Storage Account containers
* Parse data with multiple parser types (NDJSON, JSON, External)
* Enrich records with metadata using Go templates
* Write to NDJSON files with log rotation
* Delete processed blobs (configurable)

=== Authentication ✅
* Azure Service Principal authentication
* Azure Workload Identity authentication  
* Azure Managed Identity support
* Connection string authentication
* DefaultAzureCredential for automatic credential detection

=== Source Configuration ✅
* Azure Storage Account name or connection string
* Blob container name
* File pattern (case-insensitive regex) to match files
* Hierarchical path support for blob names

=== Parser System ✅
* NDJSON parser (default) - one JSON object per line, resilient to invalid lines
* JSON parser - arrays and single objects
* External parser - run custom commands for parsing
  * File mode: INPUT_FILE/OUTPUT_FILE environment variables
  * Stdin/Stdout mode: pipe data through command
* Each parser has configurable file pattern for matching
* Each external parser operation gets unique temp directory

=== Enrichment Engine ✅
* Go text/template for metadata enrichment
* Full blob metadata: name, container, storage account, size, content type
* Timestamps: processed_at, last_modified
* Hierarchical path parsing: Directory, FileName, Extension, PathParts
* Path manipulation functions: pathDir, pathBase, pathExt, pathSplit, pathJoin

=== Output Configuration ✅
* Configurable output directory and filename
* Log rotation (size-based with max_size)
* Backup retention (max_backups, max_age)
* Optional gzip compression
* First-write padding (1025 bytes minimum, unless gzip)

=== Blob Management ✅
* Sort order: oldest or newest first
* Age filtering: min-age and max-age
* Batch limiting: max blobs per processing cycle
* Once-mode limiting: configurable max for single-shot mode (default: 10)
* Delete after process option

=== Polling & Backoff ✅
* Configurable poll interval (default: 1 minute)
* Backoff based on free disk space
* Minimum free space threshold (min_free_space_gb)
* Poll metrics tracking

=== Operation Modes ✅
* Continuous mode: run indefinitely with configurable poll interval
* Once mode: process available blobs once and exit
* Dry-run mode: preview without making changes

=== Metrics ✅
Prometheus metrics at /metrics endpoint:
* adda_blobs_processed_total - Successfully processed blobs
* adda_blobs_failed_total - Failed processing attempts  
* adda_processed_bytes_total - Size of processed data
* adda_lines_written_total - Lines written to output
* adda_blob_processing_seconds - Processing time histogram
* adda_output_dir_free_bytes - Free space in output directory
* adda_active_parsers - Currently active parsers
* adda_polls_total - Total poll attempts
* adda_polls_with_data_total - Polls that found data
* adda_polls_empty_total - Polls with no data

=== Health Check ✅
* Health endpoint at /health

=== Error Handling ✅
* Retry logic for transient failures
* Per-blob error isolation
* Per-line error handling in NDJSON parser
* Graceful shutdown on signals

=== Concurrency ✅
* Configurable worker pool
* Semaphore-based concurrency control
* Thread-safe metrics and writing

== Configuration ✅

Multiple configuration sources (in order of precedence):
1. Command-line flags
2. Environment variables (ADDA_ prefix)
3. Configuration file (YAML)

== CLI Flags ✅

Source:
* --storage-account, --container, --connection-string, --file-pattern

Output:
* --output-dir, --output-file, --max-size, --max-backups, --max-age, --gzip

Processing:
* --workers, --dry-run, --delete-after-process, --once
* --sort-order, --batch-limit, --once-limit
* --blob-min-age, --blob-max-age
* --poll-interval, --temp-dir

Metrics & Logging:
* --metrics, --metrics-address
* --log-level, --log-format

== Project Layout ✅

* cmd/adda - main entry point
* cmd/adda/config - configuration handling
* cmd/adda/processor - main processing pipeline
* pkg/reader - Azure Storage Account blob reader
* pkg/parser - parser interface and registry
* pkg/parser/ndjson - NDJSON parser (default)
* pkg/parser/json - JSON array/object parser
* pkg/parser/external - external command parser
* pkg/enricher - metadata enrichment with templates
* pkg/writer - NDJSON file writer with rotation
* pkg/metrics - Prometheus metrics
* internal/utils - utility functions
* tests/e2e - end-to-end tests with Azurite
* docs/ - documentation
* scripts/ - helper scripts and example parsers
* examples/ - example configurations

== Testing ✅

* Unit tests for all core components
* E2E tests using Azurite (Azure Storage Emulator)
* Manual test scripts with min-age validation
* CSV parser examples (file and stdin/stdout modes)

== Build & Deployment ✅

* Makefile with build, test, lint targets
* Optimized builds with stripped symbols (-ldflags "-s -w")
* Docker support
* Cross-compilation for Linux
* golangci-lint integration

== Documentation ✅

* README.md - Quick start and overview
* docs/ARCHITECTURE.md - System design
* docs/CONFIGURATION.md - Complete config reference
* docs/ENRICHMENT.md - Template syntax and examples
* docs/PARSERS.md - Parser types and custom parsers
* docs/DEVELOPMENT.md - Developer guide

