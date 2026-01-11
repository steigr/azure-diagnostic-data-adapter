#!/bin/bash
# CSV to NDJSON parser for adda external parser (stdin/stdout mode)
#
# This script reads CSV data from stdin and writes NDJSON to stdout.
# First line is treated as headers.
# Each subsequent line becomes a JSON object on its own line (NDJSON format).

set -e

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
'

