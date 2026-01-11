= Azure Diagnostic Data Adapter

* Tool to read data from Azure Storage Account blob containers.
* Parses and enriches data.
* Parser can be external tool (command + args + environment variables for input file and output JSON file).
* Enrich JSON data with metadata.
* Write to NDJSON file and flush.
* Delete processed blob. 
* Log rotation.
* Backoff based on (next) data size in comparison to free space.

== Authentication

* Support Azure Service Principal authentication.
* Support Azure Workload Identity authentication.

== Source Configuration

* Azure Storage Account name, blob container name
* File pattern (Regex) to match files in blob container.
* Parser with id, command, and arguments.
* Go text-template for metadata enrichment (must produce valid JSON).

== Output Configuration

* Directory for NDJSON output files.
* Log rotation settings (size-based, time-based).

== Metrics

* Implement Prometheus metrics for monitoring:
  * Number of processed blobs.
  * Number of failed blob processing attempts.
  * Size of processed data.
  * Number of lines (items) written to output files.
  * Time taken for processing each blob.
  * Current free space in output directory.
  * Number of active parsers.

== Implementation Details

* Use Azure SDK for Go to interact with Azure Storage Account.
* Use Go's os/exec package to run external parsers.
* Use Go's text/template package for metadata enrichment.
* Use standard Go libraries for file I/O and logging.
* Implement error handling and retries for robustness.
* Ensure proper resource cleanup (e.g., closing file handles, deleting blobs).
* Consider concurrency for processing multiple blobs in parallel, if applicable.
* Implement configuration validation at startup.
* Write unit tests and integration tests for key components.
* Document usage and configuration options.
* Use in-memory pipelines for reading, processing, and writing data to minimize disk I/O where possible.
* Use state-of-the-art libraries for flag and configuration management (e.g., Cobra, Viper).
* Ensure the tool can be run as a standalone binary or as part of a larger data processing pipeline.
* Consider implementing a dry-run mode for testing configurations without actual data processing.
* Implement logging with different verbosity levels (info, debug, error).

== Layout

* cmd/adda - main entry point for the tool.
* cmd/adda/config - configuration handling.
* pkg/reader - Azure Storage Account blob reader.
* pkg/parser - external parser handling.
* pkg/parser/ndjson - NDJSON parser implementation (default for .json files).
* pkg/parser/json - JSON array/object parser implementation.
* pkg/parser/external - external command parser implementation.
* pkg/enricher - metadata enrichment.
* pkg/writer - NDJSON file writer with log rotation.
* pkg/metrics - Prometheus metrics implementation.
* internal/utils - utility functions.
* tests/ - unit and integration tests.
* docs/ - documentation.
* scripts/ - helper scripts for setup and deployment.
* examples/ - example configurations and usage scenarios.
* Makefile - build and test automation.
* Dockerfile - containerization for deployment.
