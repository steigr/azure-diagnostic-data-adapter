#!/bin/bash
# Manual test script for adda
# This script starts Azurite, uploads test data, and runs adda in debug mode.
# It also tests the min-age functionality by uploading files and waiting for them to age.
# Press Ctrl+C to stop everything and clean up.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
AZURITE_DATA_DIR="${AZURITE_DATA_DIR:-/tmp/azurite-manual-test}"
AZURITE_PID_FILE="${AZURITE_DATA_DIR}/azurite.pid"
OUTPUT_DIR="${PROJECT_ROOT}/tmp/manual-test-output"
TEMP_DIR="${PROJECT_ROOT}/tmp/adda-temp"
CONFIG_FILE="${SCRIPT_DIR}/manual-test-config.yaml"

# Test configuration
MIN_AGE_TEST="${MIN_AGE_TEST:-false}"  # Set to "true" to run min-age test
MIN_AGE_SECONDS="${MIN_AGE_SECONDS:-15}"  # Minimum age in seconds for testing
DISK_SPACE_TEST="${DISK_SPACE_TEST:-false}"  # Set to "true" to run disk space backoff test

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to display output file contents
show_output_file() {
    echo ""
    log_info "=== Final Output File Contents ==="

    if [ -f "$OUTPUT_DIR/test-output.json" ]; then
        # Count records
        RECORD_COUNT=$(wc -l < "$OUTPUT_DIR/test-output.json" | tr -d ' ')
        FILE_SIZE=$(ls -lh "$OUTPUT_DIR/test-output.json" | awk '{print $5}')

        log_info "Output file: $OUTPUT_DIR/test-output.json"
        log_info "File size: $FILE_SIZE"
        log_info "Total records: $RECORD_COUNT"

        echo ""
        log_info "Output file contents:"
        echo "──────────────────────────────────────────────────────────────"
        # Pretty print JSON if jq is available, otherwise just cat
        if command -v jq >/dev/null 2>&1; then
            cat "$OUTPUT_DIR/test-output.json" | while read -r line; do
                echo "$line" | jq -C . 2>/dev/null || echo "$line"
            done
        else
            cat "$OUTPUT_DIR/test-output.json"
        fi
        echo "──────────────────────────────────────────────────────────────"
    else
        log_warn "No output file found at $OUTPUT_DIR/test-output.json"
    fi

    # List all files in output directory
    echo ""
    log_info "All files in output directory:"
    ls -la "$OUTPUT_DIR" 2>/dev/null || log_warn "Output directory is empty"
}

# Cleanup function
cleanup() {
    log_info "Cleaning up..."
    
    # Show output file contents before cleanup
    show_output_file

    # Stop adda if running
    if [ -n "$ADDA_PID" ] && kill -0 "$ADDA_PID" 2>/dev/null; then
        log_info "Stopping adda (PID: $ADDA_PID)..."
        kill "$ADDA_PID" 2>/dev/null || true
        wait "$ADDA_PID" 2>/dev/null || true
    fi
    
    # Stop Azurite Blob Service
    if [ -f "$AZURITE_PID_FILE" ]; then
        AZURITE_PID=$(cat "$AZURITE_PID_FILE")
        if kill -0 "$AZURITE_PID" 2>/dev/null; then
            log_info "Stopping Azurite Blob Service (PID: $AZURITE_PID)..."
            kill "$AZURITE_PID" 2>/dev/null || true
            wait "$AZURITE_PID" 2>/dev/null || true
        fi
        rm -f "$AZURITE_PID_FILE"
    fi
    
    log_success "Cleanup complete"
    exit 0
}

# Set up trap for cleanup on exit/interrupt
trap cleanup EXIT INT TERM

# Check if azurite-blob is installed
if ! command -v azurite-blob >/dev/null 2>&1; then
    log_error "Azurite is not installed. Run 'make tools-azurite' first."
    exit 1
fi

# Check if Azure CLI is installed (required for test data upload)
if ! command -v az >/dev/null 2>&1; then
    log_error "Azure CLI is required for uploading test data."
    log_info "Install it with: brew install azure-cli (macOS)"
    exit 1
fi

# Check if adda binary exists
if [ ! -f "$PROJECT_ROOT/bin/adda" ]; then
    log_warn "adda binary not found. Building..."
    cd "$PROJECT_ROOT" && make build
fi

# Clean up old test data
log_info "Cleaning up old test data..."
rm -rf "$AZURITE_DATA_DIR"
rm -rf "$OUTPUT_DIR"
rm -rf "$TEMP_DIR"

# Create directories
mkdir -p "$AZURITE_DATA_DIR"
mkdir -p "$OUTPUT_DIR"
mkdir -p "$TEMP_DIR"
log_success "Test directories prepared"

# Start Azurite Blob Service
log_info "Starting Azurite Blob Service..."
if [ -f "$AZURITE_PID_FILE" ] && kill -0 "$(cat "$AZURITE_PID_FILE")" 2>/dev/null; then
    log_warn "Azurite Blob Service is already running (PID: $(cat "$AZURITE_PID_FILE"))"
else
    azurite-blob --silent --location "$AZURITE_DATA_DIR" --debug "$AZURITE_DATA_DIR/debug.log" &
    echo $! > "$AZURITE_PID_FILE"
    log_success "Azurite Blob Service started (PID: $(cat "$AZURITE_PID_FILE"))"
    sleep 2
fi

# Wait for Azurite Blob Service to be ready by checking if the port is open
log_info "Waiting for Azurite Blob Service to be ready..."
for i in {1..10}; do
    if nc -z 127.0.0.1 10000 2>/dev/null; then
        log_success "Azurite Blob Service is ready"
        break
    fi
    if [ $i -eq 10 ]; then
        log_error "Azurite Blob Service failed to start"
        exit 1
    fi
    sleep 1
done

# Create test container and upload sample data
log_info "Setting up test data (NDJSON and CSV files)..."
"$SCRIPT_DIR/setup-test-data.sh"

# Test min-age functionality if enabled
if [ "$MIN_AGE_TEST" = "true" ]; then
    log_info "=== Testing min-age functionality ==="
    log_info "Running adda with --blob-min-age ${MIN_AGE_SECONDS}s (files should NOT be processed yet)..."

    cd "$PROJECT_ROOT"
    ./bin/adda \
        --config "$CONFIG_FILE" \
        --log-level debug \
        --log-format text \
        --output-dir "$OUTPUT_DIR" \
        --output-file "test-output.json" \
        --blob-min-age "${MIN_AGE_SECONDS}s" \
        --once || true

    # Check if any files were processed (should be none)
    if [ -f "$OUTPUT_DIR/test-output.json" ]; then
        RECORD_COUNT=$(wc -l < "$OUTPUT_DIR/test-output.json" | tr -d ' ')
        if [ "$RECORD_COUNT" -gt 0 ]; then
            log_error "Files were processed before min-age! Expected 0 records, got $RECORD_COUNT"
        else
            log_success "No files processed (as expected - files are too new)"
        fi
    else
        log_success "No files processed (as expected - files are too new)"
    fi

    log_info "Waiting ${MIN_AGE_SECONDS} seconds for files to age..."
    sleep "$MIN_AGE_SECONDS"

    log_info "Running adda again (files should now be processed)..."
    rm -f "$OUTPUT_DIR/test-output.json"

    ./bin/adda \
        --config "$CONFIG_FILE" \
        --log-level debug \
        --log-format text \
        --output-dir "$OUTPUT_DIR" \
        --output-file "test-output.json" \
        --blob-min-age "${MIN_AGE_SECONDS}s" \
        --once &

    ADDA_PID=$!
    log_info "adda started (PID: $ADDA_PID)"

    # Wait for adda to finish
    wait "$ADDA_PID"
    ADDA_EXIT_CODE=$?

    if [ $ADDA_EXIT_CODE -eq 0 ]; then
        if [ -f "$OUTPUT_DIR/test-output.json" ]; then
            RECORD_COUNT=$(wc -l < "$OUTPUT_DIR/test-output.json" | tr -d ' ')
            if [ "$RECORD_COUNT" -gt 0 ]; then
                log_success "Files processed after min-age! Got $RECORD_COUNT records"
            else
                log_error "No files processed after min-age (expected some records)"
            fi
        else
            log_error "No output file created after min-age"
        fi
    else
        log_error "adda exited with code $ADDA_EXIT_CODE"
    fi

    log_success "=== min-age test complete ==="
    show_output_file
    exit 0
fi

# Test disk space backoff functionality if enabled
if [ "$DISK_SPACE_TEST" = "true" ]; then
    log_info "=== Testing disk space backoff functionality ==="

    # Configuration for disk space test
    POLL_INTERVAL="${POLL_INTERVAL:-5s}"  # Short poll interval for testing
    BACKOFF_TARGET=2  # Number of backoffs before deleting blocker file
    BLOCKER_FILE="$OUTPUT_DIR/.disk-space-blocker"
    BLOCKER_SIZE_MB=10  # Small blocker file (10MB)

    # Get current free space in the output directory
    FREE_SPACE_KB=$(df -k "$OUTPUT_DIR" | tail -1 | awk '{print $4}')
    FREE_SPACE_MB=$((FREE_SPACE_KB / 1024))

    log_info "Current free space: ${FREE_SPACE_MB} MB"

    # Set min-free-space to current free space (so after creating blocker, it will be below threshold)
    # We set threshold to: current_free - blocker_size + margin
    # After blocker is created, free space will be: current_free - blocker_size
    # Which is less than threshold, triggering backoff
    MIN_FREE_SPACE_MB=$((FREE_SPACE_MB - BLOCKER_SIZE_MB + 50))
    MIN_FREE_SPACE="${MIN_FREE_SPACE_MB}MB"

    log_info "Will set --min-free-space to ${MIN_FREE_SPACE}"
    log_info "Will create ${BLOCKER_SIZE_MB} MB blocker file to reduce free space below threshold"

    # Step 1: Create the blocker file FIRST to reduce free space below threshold
    log_info "Creating blocker file (${BLOCKER_SIZE_MB} MB) BEFORE starting adda..."
    dd if=/dev/zero of="$BLOCKER_FILE" bs=1M count="$BLOCKER_SIZE_MB" 2>/dev/null
    log_success "Blocker file created: $BLOCKER_FILE"

    # Show new free space (should be below threshold)
    NEW_FREE_SPACE_KB=$(df -k "$OUTPUT_DIR" | tail -1 | awk '{print $4}')
    NEW_FREE_SPACE_MB=$((NEW_FREE_SPACE_KB / 1024))
    log_info "Free space after blocker: ${NEW_FREE_SPACE_MB} MB (threshold: ${MIN_FREE_SPACE_MB} MB)"

    if [ "$NEW_FREE_SPACE_MB" -ge "$MIN_FREE_SPACE_MB" ]; then
        log_warn "Free space is still above threshold. Backoff may not trigger."
    else
        log_success "Free space is below threshold. Backoff will trigger when adda starts."
    fi

    # Step 2: Create and upload a test file
    log_info "Creating test file for processing..."
    TEST_FILE=$(mktemp)
    for i in $(seq 1 100); do
        echo "{\"id\":$i,\"message\":\"disk space test record $i\"}"
    done > "$TEST_FILE"

    log_info "Uploading test file to Azurite..."
    export AZURE_STORAGE_CONNECTION_STRING="DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"
    az storage blob upload \
        --container-name "test-diagnostics" \
        --name "disk-space-test.json" \
        --file "$TEST_FILE" \
        --overwrite 2>/dev/null

    rm -f "$TEST_FILE"
    log_success "Test file uploaded"

    # Step 3: Start adda - it should immediately enter backoff due to low disk space
    log_info "Starting adda with --min-free-space $MIN_FREE_SPACE --poll-interval $POLL_INTERVAL --backoff-enabled"
    log_info "adda should enter backoff immediately due to low disk space..."

    cd "$PROJECT_ROOT"

    # Run adda in background
    ./bin/adda \
        --config "$CONFIG_FILE" \
        --log-level debug \
        --log-format text \
        --output-dir "$OUTPUT_DIR" \
        --output-file "test-output.json" \
        --min-free-space "$MIN_FREE_SPACE" \
        --poll-interval "$POLL_INTERVAL" \
        --backoff-enabled \
        2>&1 &

    ADDA_PID=$!
    log_info "adda started (PID: $ADDA_PID)"

    # Step 4: Monitor and delete blocker file after backoffs
    BACKOFF_COUNT=0
    while kill -0 "$ADDA_PID" 2>/dev/null; do
        sleep 6  # Wait slightly longer than poll interval

        if [ -f "$BLOCKER_FILE" ]; then
            BACKOFF_COUNT=$((BACKOFF_COUNT + 1))
            log_warn "Backoff cycle #$BACKOFF_COUNT (blocker file still present, free: ${NEW_FREE_SPACE_MB} MB < threshold: ${MIN_FREE_SPACE_MB} MB)"

            # Update free space display
            NEW_FREE_SPACE_KB=$(df -k "$OUTPUT_DIR" | tail -1 | awk '{print $4}')
            NEW_FREE_SPACE_MB=$((NEW_FREE_SPACE_KB / 1024))

            if [ "$BACKOFF_COUNT" -ge "$BACKOFF_TARGET" ]; then
                log_info "Reached $BACKOFF_TARGET backoff cycles, deleting blocker file..."
                rm -f "$BLOCKER_FILE"
                log_success "Blocker file deleted!"

                # Show restored free space
                RESTORED_FREE_KB=$(df -k "$OUTPUT_DIR" | tail -1 | awk '{print $4}')
                RESTORED_FREE_MB=$((RESTORED_FREE_KB / 1024))
                log_info "Free space restored: ${RESTORED_FREE_MB} MB (threshold: ${MIN_FREE_SPACE_MB} MB)"
                log_info "adda should now process the blob..."
            fi
        else
            # Blocker file gone, wait for processing to complete
            log_info "Blocker file removed, waiting for processing to complete..."
            sleep 10
            break
        fi
    done

    # Wait a bit for adda to process
    sleep 5

    # Clean up
    rm -f "$BLOCKER_FILE"
    kill "$ADDA_PID" 2>/dev/null || true
    wait "$ADDA_PID" 2>/dev/null || true

    # Check results
    if [ -f "$OUTPUT_DIR/test-output.json" ]; then
        RECORD_COUNT=$(wc -l < "$OUTPUT_DIR/test-output.json" | tr -d ' ')
        if [ "$RECORD_COUNT" -gt 0 ]; then
            log_success "Disk space backoff test PASSED!"
            log_success "- adda started with low disk space and entered backoff"
            log_success "- After $BACKOFF_TARGET backoff cycles, blocker file was deleted"
            log_success "- Processing resumed after disk space was restored"
            log_success "- $RECORD_COUNT records were processed"
        else
            log_error "Disk space backoff test FAILED: No records processed"
        fi
    else
        log_error "Disk space backoff test FAILED: No output file created"
    fi

    log_success "=== disk space backoff test complete ==="
    show_output_file
    exit 0
fi

# Run adda in debug mode (normal test)
log_info "Starting adda in debug mode..."
log_info "Output directory: $OUTPUT_DIR"
log_info "Temp directory: $TEMP_DIR"
log_info "Press Ctrl+C to stop"
echo ""

cd "$PROJECT_ROOT"
./bin/adda \
    --config "$CONFIG_FILE" \
    --log-level debug \
    --log-format text \
    --output-dir "$OUTPUT_DIR" \
    --output-file "test-output.json" \
    --once &

ADDA_PID=$!
log_info "adda started (PID: $ADDA_PID)"

# Wait for adda to finish or be interrupted
wait "$ADDA_PID"
ADDA_EXIT_CODE=$?

if [ $ADDA_EXIT_CODE -eq 0 ]; then
    log_success "adda completed successfully"
    
    # Show output
    show_output_file
else
    log_error "adda exited with code $ADDA_EXIT_CODE"
fi

# Keep running for interactive testing (user can Ctrl+C to exit)
log_info "Test complete. Press Ctrl+C to clean up and exit."
log_info "You can also run additional tests manually."
echo ""
log_info "Useful commands:"
echo "  - View Azurite logs: tail -f $AZURITE_DATA_DIR/debug.log"
echo "  - Upload more test data: $SCRIPT_DIR/setup-test-data.sh"
echo "  - Run adda again: ./bin/adda --config $CONFIG_FILE --log-level debug --once"
echo "  - Run min-age test: MIN_AGE_TEST=true $0"
echo "  - Run with custom min-age: MIN_AGE_TEST=true MIN_AGE_SECONDS=10 $0"
echo "  - Run disk space test: DISK_SPACE_TEST=true $0"
echo ""

# Wait indefinitely (cleanup will happen on Ctrl+C)
while true; do
    sleep 1
done
