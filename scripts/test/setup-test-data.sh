#!/bin/bash
# Setup test data in Azurite for manual testing

set -e

# Azurite connection string
CONNECTION_STRING="DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"
CONTAINER_NAME="test-diagnostics"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if az CLI is available
if ! command -v az >/dev/null 2>&1; then
    log_error "Azure CLI is required but not installed."
    log_info "Install it with: brew install azure-cli (macOS) or see https://docs.microsoft.com/en-us/cli/azure/install-azure-cli"
    exit 1
fi

log_info "Using Azure CLI to upload test data..."

# Create container
az storage container create \
    --name "$CONTAINER_NAME" \
    --connection-string "$CONNECTION_STRING" \
    2>/dev/null || true

# Upload NDJSON test data
TEMP_FILE=$(mktemp)
cat > "$TEMP_FILE" << 'EOF'
{"timestamp":"2026-01-11T10:00:00Z","level":"INFO","message":"Application started","service":"test-service"}
{"timestamp":"2026-01-11T10:00:01Z","level":"DEBUG","message":"Initializing components","service":"test-service"}
{"timestamp":"2026-01-11T10:00:02Z","level":"INFO","message":"Connected to database","service":"test-service"}
{"timestamp":"2026-01-11T10:00:03Z","level":"WARN","message":"High memory usage detected","service":"test-service","memory_percent":85}
{"timestamp":"2026-01-11T10:00:04Z","level":"ERROR","message":"Failed to connect to external API","service":"test-service","error":"timeout"}
EOF

BLOB_NAME="logs/app-$(date +%Y%m%d-%H%M%S).json"
az storage blob upload \
    --container-name "$CONTAINER_NAME" \
    --name "$BLOB_NAME" \
    --file "$TEMP_FILE" \
    --connection-string "$CONNECTION_STRING" \
    --overwrite >/dev/null

log_success "Uploaded NDJSON test data: $BLOB_NAME"
rm -f "$TEMP_FILE"

# Upload CSV test data (for external parser testing)
TEMP_FILE=$(mktemp)
cat > "$TEMP_FILE" << 'EOF'
id,name,email,department,salary
1,John Doe,john.doe@example.com,Engineering,75000
2,Jane Smith,jane.smith@example.com,Marketing,68000
3,Bob Johnson,bob.johnson@example.com,Engineering,82000
4,Alice Williams,alice.williams@example.com,HR,55000
5,Charlie Brown,charlie.brown@example.com,Sales,71000
EOF

BLOB_NAME="data/employees-$(date +%Y%m%d-%H%M%S).csv"
az storage blob upload \
    --container-name "$CONTAINER_NAME" \
    --name "$BLOB_NAME" \
    --file "$TEMP_FILE" \
    --connection-string "$CONNECTION_STRING" \
    --overwrite >/dev/null

log_success "Uploaded CSV test data: $BLOB_NAME"
rm -f "$TEMP_FILE"

# Upload another CSV with different structure
TEMP_FILE=$(mktemp)
cat > "$TEMP_FILE" << 'EOF'
timestamp,event_type,user_id,action,status
2026-01-11T08:00:00Z,login,user123,authenticate,success
2026-01-11T08:15:00Z,page_view,user123,view_dashboard,success
2026-01-11T08:30:00Z,api_call,user123,fetch_data,success
2026-01-11T08:45:00Z,logout,user123,sign_out,success
2026-01-11T09:00:00Z,login,user456,authenticate,failed
EOF

BLOB_NAME="events/user-events-$(date +%Y%m%d-%H%M%S).csv"
az storage blob upload \
    --container-name "$CONTAINER_NAME" \
    --name "$BLOB_NAME" \
    --file "$TEMP_FILE" \
    --connection-string "$CONNECTION_STRING" \
    --overwrite >/dev/null

log_success "Uploaded CSV events data: $BLOB_NAME"
rm -f "$TEMP_FILE"

# Upload single-line NDJSON (edge case)
TEMP_FILE=$(mktemp)
echo '{"single":"line","test":true}' > "$TEMP_FILE"

BLOB_NAME="single/record-$(date +%Y%m%d-%H%M%S).json"
az storage blob upload \
    --container-name "$CONTAINER_NAME" \
    --name "$BLOB_NAME" \
    --file "$TEMP_FILE" \
    --connection-string "$CONNECTION_STRING" \
    --overwrite >/dev/null

log_success "Uploaded single-line NDJSON: $BLOB_NAME"
rm -f "$TEMP_FILE"

# Upload TSV test data (for stdin/stdout external parser testing)
TEMP_FILE=$(mktemp)
cat > "$TEMP_FILE" << 'EOF'
product_id,product_name,category,price,quantity
SKU001,Wireless Mouse,Electronics,29.99,150
SKU002,USB Keyboard,Electronics,49.99,75
SKU003,Office Chair,Furniture,299.99,25
SKU004,Desk Lamp,Lighting,39.99,100
SKU005,Monitor Stand,Accessories,59.99,50
EOF

BLOB_NAME="inventory/products-$(date +%Y%m%d-%H%M%S).tsv"
az storage blob upload \
    --container-name "$CONTAINER_NAME" \
    --name "$BLOB_NAME" \
    --file "$TEMP_FILE" \
    --connection-string "$CONNECTION_STRING" \
    --overwrite >/dev/null

log_success "Uploaded TSV test data (stdin/stdout parser): $BLOB_NAME"
rm -f "$TEMP_FILE"

log_success "All test data uploaded successfully"

# List blobs
log_info "Blobs in container '$CONTAINER_NAME':"
az storage blob list \
    --container-name "$CONTAINER_NAME" \
    --connection-string "$CONNECTION_STRING" \
    --output table
