#!/bin/bash
# CSV to NDJSON parser for adda external parser (file mode)
#
# Environment variables:
#   INPUT_FILE  - Path to the input CSV file
#   OUTPUT_FILE - Path where NDJSON output should be written
#   TEMP_DIR    - Temporary directory for this parsing operation
#
# The script reads a CSV file and converts it to NDJSON format.
# First line is treated as headers.
# Each subsequent line becomes a JSON object on its own line.

set -e

if [ -z "$INPUT_FILE" ] || [ -z "$OUTPUT_FILE" ]; then
    echo "ERROR: INPUT_FILE and OUTPUT_FILE environment variables must be set" >&2
    exit 1
fi

if [ ! -f "$INPUT_FILE" ]; then
    echo "ERROR: Input file not found: $INPUT_FILE" >&2
    exit 1
fi

# Check if file is empty
if [ ! -s "$INPUT_FILE" ]; then
    # Empty file, create empty output
    touch "$OUTPUT_FILE"
    exit 0
fi

# Use awk to convert CSV to NDJSON (one JSON object per line)
awk -F',' '
NR == 1 {
    # Store headers
    for (i = 1; i <= NF; i++) {
        # Remove quotes and whitespace from headers
        gsub(/^[[:space:]]*"|"[[:space:]]*$/, "", $i)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", $i)
        headers[i] = $i
    }
    num_fields = NF
    next
}
{
    # Build JSON object for each record
    printf "{"
    for (i = 1; i <= num_fields; i++) {
        # Remove surrounding quotes from value
        gsub(/^[[:space:]]*"|"[[:space:]]*$/, "", $i)
        
        # Escape special JSON characters
        gsub(/\\/, "\\\\", $i)
        gsub(/"/, "\\\"", $i)
        gsub(/\t/, "\\t", $i)
        gsub(/\r/, "", $i)
        
        if (i > 1) printf ","
        printf "\"%s\":\"%s\"", headers[i], $i
    }
    print "}"
}
' "$INPUT_FILE" > "$OUTPUT_FILE"
