# Parsers

This document describes the parser system in the Azure Diagnostic Data Adapter (adda).

## Overview

Parsers convert raw blob content into structured records (JSON objects). Each parser can be configured with a file pattern (regex) to match specific blob names.

## Parser Types

### NDJSON Parser (Default)

The NDJSON (Newline Delimited JSON) parser is the default and most commonly used parser. It handles data where each line contains a single JSON object.

**Use case:** Log files, event streams, and most Azure diagnostic data.

**Configuration:**
```yaml
parsers:
  - id: "ndjson"
    type: "ndjson"
    file_pattern: ".*\\.json$"
```

**Example input:**
```
{"timestamp":"2026-01-11T10:00:00Z","level":"INFO","message":"Application started"}
{"timestamp":"2026-01-11T10:00:01Z","level":"DEBUG","message":"Loading config"}
{"timestamp":"2026-01-11T10:00:02Z","level":"ERROR","message":"Connection failed"}
```

**Features:**
- Parses one JSON object per line
- Skips empty lines and whitespace-only lines
- **Resilient parsing:** Invalid lines are logged and skipped (processing continues)
- Case-insensitive file pattern matching

**Error handling:**
When a line fails to parse, adda logs a warning with:
- Line number
- Error message
- Content of the invalid line

Processing continues with the next line.

### JSON Parser

The JSON parser handles traditional JSON files containing arrays or single objects.

**Use case:** JSON files containing arrays of records or single JSON objects.

**Configuration:**
```yaml
parsers:
  - id: "json-array"
    type: "json"
    file_pattern: ".*\\.json-array$"
```

**Example input (array):**
```json
[
  {"id": 1, "name": "Alice"},
  {"id": 2, "name": "Bob"},
  {"id": 3, "name": "Charlie"}
]
```

**Example input (single object):**
```json
{"id": 1, "name": "Alice", "data": {"nested": "value"}}
```

**Features:**
- Parses JSON arrays (returns each element as a record)
- Parses single JSON objects (returns one record)
- Preserves nested structures

**Note:** This parser does NOT support NDJSON format. Use the `ndjson` parser for line-delimited JSON.

### External Parser

The external parser delegates parsing to an external command, enabling support for any file format.

**Use case:** CSV, XML, custom formats, or complex transformations.

#### File Mode (Default)

The external command reads from a file and writes to a file:

**Configuration:**
```yaml
parsers:
  - id: "csv"
    type: "external"
    file_pattern: ".*\\.csv$"
    command: "bash"
    args: ["./scripts/parsers/csv-to-json.sh"]
    env:
      CUSTOM_VAR: "value"
```

**Environment variables provided:**
| Variable | Description |
|----------|-------------|
| `INPUT_FILE` | Path to the input file (contains blob data) |
| `OUTPUT_FILE` | Path where NDJSON output should be written |
| `TEMP_DIR` | Unique temporary directory for this operation |

**Expected output format:** NDJSON (one JSON object per line)

**Example script:**
```bash
#!/bin/bash
# csv-to-json.sh

set -e

awk -F',' '
NR == 1 { for (i=1; i<=NF; i++) headers[i] = $i; next }
{
    printf "{"
    for (i=1; i<=NF; i++) {
        if (i > 1) printf ","
        printf "\"%s\":\"%s\"", headers[i], $i
    }
    print "}"
}
' "$INPUT_FILE" > "$OUTPUT_FILE"
```

#### Stdin/Stdout Mode

For streaming parsers, use stdin/stdout mode:

**Configuration:**
```yaml
parsers:
  - id: "csv-stream"
    type: "external"
    file_pattern: ".*\\.tsv$"
    command: "bash"
    args: ["./scripts/parsers/csv-to-json-stdio.sh"]
    stdin: true
    stdout: true
```

**Behavior:**
- Input data is piped to the command's stdin
- Output is read from the command's stdout
- No files are created (more efficient for simple transformations)

**Example script:**
```bash
#!/bin/bash
# csv-to-json-stdio.sh
# Reads CSV from stdin, writes NDJSON to stdout

awk -F',' '
NR == 1 { for (i=1; i<=NF; i++) headers[i] = $i; next }
{
    printf "{"
    for (i=1; i<=NF; i++) {
        if (i > 1) printf ","
        printf "\"%s\":\"%s\"", headers[i], $i
    }
    print "}"
}
'
```

## Parser Selection

Parsers are matched against blob names in the order they are defined. The first parser whose `file_pattern` matches the blob name is used.

**Example configuration with multiple parsers:**
```yaml
parsers:
  # Match .ndjson files
  - id: "ndjson-files"
    type: "ndjson"
    file_pattern: ".*\\.ndjson$"
  
  # Match .json files (also as NDJSON - most common)
  - id: "json-files"
    type: "ndjson"
    file_pattern: ".*\\.json$"
  
  # Match .csv files
  - id: "csv-files"
    type: "external"
    file_pattern: ".*\\.csv$"
    command: "csv-parser"
  
  # Catch-all for remaining files (no pattern = matches all)
  - id: "default"
    type: "ndjson"
```

### Pattern Matching

- Patterns are **case-insensitive** (e.g., `.*\.JSON$` matches `file.json` and `FILE.JSON`)
- Patterns are matched against the full blob path (e.g., `logs/2026/data.json`)
- If no pattern is specified, the parser matches all files

**Common patterns:**
| Pattern | Matches |
|---------|---------|
| `.*\.json$` | All .json files |
| `.*\.(json\|ndjson)$` | .json and .ndjson files |
| `logs/.*\.json$` | .json files in logs/ directory |
| `.*` | All files |

## Best Practices

1. **Order parsers from specific to general** - Put the most specific patterns first

2. **Use NDJSON as default** - Most log and diagnostic data is NDJSON formatted

3. **Test with --dry-run** - Preview which parser will be used for each file:
   ```bash
   adda --config config.yaml --dry-run
   ```

4. **Handle errors gracefully** - External parsers should:
   - Exit with non-zero status on errors
   - Write meaningful error messages to stderr
   - Never produce partial/invalid output

5. **Use stdin/stdout for simple transformations** - It's more efficient than file I/O

## Troubleshooting

### No Parser Matches

If no parser matches a blob, it will be skipped with a debug log message:
```
no matching parser for blob name=logs/unknown.xyz
```

**Solution:** Add a parser with a matching file pattern.

### Parser Errors

External parser errors are logged with:
- Exit code
- Command output (trimmed)

**Example:**
```
failed to parse blob blob=data.csv error="external command failed: exit status 1, output: Invalid CSV format"
```

### Invalid Output

If a parser produces invalid JSON, you'll see parse errors. Verify:
1. The parser outputs valid JSON/NDJSON
2. Each line is a complete JSON object (for NDJSON)
3. The output file exists (for file mode)

