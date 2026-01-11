# End-to-End Tests

This directory contains end-to-end tests that use the [Azurite](https://github.com/Azure/Azurite) emulator for local Azure Storage testing.

## Prerequisites

### Install Azurite

You can install Azurite using npm:

```bash
npm install -g azurite
```

Or use Docker:

```bash
docker pull mcr.microsoft.com/azure-storage/azurite
```

### Start Azurite

Using npm:

```bash
azurite --silent --location /tmp/azurite --debug /tmp/azurite/debug.log
```

Using Docker:

```bash
docker run -p 10000:10000 -p 10001:10001 -p 10002:10002 \
    mcr.microsoft.com/azure-storage/azurite
```

## Running Tests

Once Azurite is running, you can run the e2e tests:

```bash
# Run all e2e tests
go test -v ./tests/e2e/...

# Run a specific test
go test -v ./tests/e2e/... -run TestE2E_ReadJSONBlob

# Run with race detection
go test -race -v ./tests/e2e/...
```

## Test Categories

### Basic Operations
- `TestE2E_ReadJSONBlob` - Read and parse a JSON array blob
- `TestE2E_ReadNDJSONBlob` - Read and parse a newline-delimited JSON blob
- `TestE2E_DeleteAfterProcess` - Verify blob deletion works correctly
- `TestE2E_FilePatternFilter` - Test regex pattern filtering of blobs

### Processing Pipeline
- `TestE2E_EnrichAndWrite` - Full pipeline: read, parse, enrich, and write
- `TestE2E_LargeBlob` - Handle large blobs with many records
- `TestE2E_GzipOutput` - Verify gzip output works correctly

### External Parsers
- `TestE2E_ExternalCSVParser` - Test CSV parsing with external script
- `TestE2E_MultipleParserTypes` - Use multiple parser types together
- `TestCSVParserScript` - Direct test of the CSV parser script
- `TestCSVParserScript_EmptyFile` - Handle empty CSV files
- `TestCSVParserScript_HeadersOnly` - Handle CSV with headers only

## Connection String

The tests use the default Azurite connection string:

```
DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;
```

## CSV Parser Script

The `scripts/parsers/csv-to-json.sh` script is an example external parser that converts CSV files to JSON. It demonstrates how to implement custom parsers for use with the `external` parser type.

### Usage

```yaml
parsers:
  - id: "csv"
    type: "external"
    file_pattern: ".*\\.csv$"
    command: "/path/to/csv-to-json.sh"
```

### Environment Variables

The script receives:
- `INPUT_FILE` - Path to the input CSV file
- `OUTPUT_FILE` - Path where JSON output should be written
- `TEMP_DIR` - Temporary directory for this parsing operation

## Skipping Tests

If Azurite is not running, the tests will be automatically skipped with a message indicating that Azurite is not available.
