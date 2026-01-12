// Package e2e provides end-to-end tests using the Azurite emulator.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/enricher"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/external"
	jsonparser "github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/json"
	ndjsonparser "github.com/steigr/azure-diagnostic-data-adapter/pkg/parser/ndjson"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/reader"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/writer"
)

const (
	// Azurite default connection string
	azuriteConnectionString = "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;"
	testContainerName       = "test-diagnostics"
)

// skipIfNoAzurite skips the test if Azurite is not running.
func skipIfNoAzurite(t *testing.T) {
	t.Helper()

	client, err := azblob.NewClientFromConnectionString(azuriteConnectionString, nil)
	if err != nil {
		t.Skipf("Skipping test: failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to list containers to check if Azurite is running
	pager := client.NewListContainersPager(nil)
	_, err = pager.NextPage(ctx)
	if err != nil {
		t.Skipf("Skipping test: Azurite is not running: %v", err)
	}
}

// setupTestContainer creates a test container and returns a cleanup function.
func setupTestContainer(t *testing.T, ctx context.Context) (*azblob.Client, func()) {
	t.Helper()

	client, err := azblob.NewClientFromConnectionString(azuriteConnectionString, nil)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Create container
	_, err = client.CreateContainer(ctx, testContainerName, nil)
	if err != nil && !strings.Contains(err.Error(), "ContainerAlreadyExists") {
		t.Fatalf("Failed to create container: %v", err)
	}

	cleanup := func() {
		// Delete all blobs in container
		pager := client.NewListBlobsFlatPager(testContainerName, nil)
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				break
			}
			for _, blob := range resp.Segment.BlobItems {
				_, _ = client.DeleteBlob(ctx, testContainerName, *blob.Name, nil)
			}
		}
		// Delete container
		_, _ = client.DeleteContainer(ctx, testContainerName, nil)
	}

	return client, cleanup
}

// uploadTestBlob uploads a blob with the given content.
func uploadTestBlob(t *testing.T, ctx context.Context, client *azblob.Client, blobName string, content []byte) {
	t.Helper()

	_, err := client.UploadBuffer(ctx, testContainerName, blobName, content, nil)
	if err != nil {
		t.Fatalf("Failed to upload blob %s: %v", blobName, err)
	}
}

func TestE2E_ReadJSONBlob(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test JSON blob
	testData := []byte(`[{"name":"test1","value":100},{"name":"test2","value":200}]`)
	uploadTestBlob(t, ctx, client, "data/test.json", testData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, `.*\.json$`)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// List blobs
	blobs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs: %v", err)
	}

	if len(blobs) != 1 {
		t.Fatalf("Expected 1 blob, got %d", len(blobs))
	}

	if blobs[0].Name != "data/test.json" {
		t.Errorf("Unexpected blob name: %s", blobs[0].Name)
	}

	// Download blob
	data, size, err := r.Download(ctx, blobs[0].Name)
	if err != nil {
		t.Fatalf("Failed to download blob: %v", err)
	}
	defer func() { _ = data.Close() }()

	if size != int64(len(testData)) {
		t.Errorf("Unexpected size: got %d, want %d", size, len(testData))
	}

	// Parse with JSON parser
	parser, err := jsonparser.New("json", `.*\.json$`)
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records, got %d", len(records))
	}

	if records[0]["name"] != "test1" {
		t.Errorf("Unexpected name: %v", records[0]["name"])
	}
}

func TestE2E_ReadNDJSONBlob(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test NDJSON blob
	testData := []byte(`{"event":"login","user":"alice"}
{"event":"logout","user":"bob"}
{"event":"login","user":"charlie"}`)
	uploadTestBlob(t, ctx, client, "logs/events.json", testData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download and parse
	data, _, err := r.Download(ctx, "logs/events.json")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	parser, err := ndjsonparser.New("ndjson", "")
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("Expected 3 records, got %d", len(records))
	}
}

func TestE2E_EnrichAndWrite(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test blob
	testData := []byte(`{"message":"hello world"}`)
	uploadTestBlob(t, ctx, client, "test/message.json", testData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download and parse
	data, size, err := r.Download(ctx, "test/message.json")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	parser, err := ndjsonparser.New("ndjson", "")
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Create enricher
	enrichTemplate := `{"_source": {"blob": "{{ .Metadata.BlobName }}", "container": "{{ .Metadata.ContainerName }}"}}`
	e, err := enricher.New(enrichTemplate)
	if err != nil {
		t.Fatalf("Failed to create enricher: %v", err)
	}

	// Enrich records
	metadata := enricher.Metadata{
		BlobName:       "test/message.json",
		ContainerName:  testContainerName,
		StorageAccount: "devstoreaccount1",
		ProcessedAt:    time.Now(),
		Size:           size,
	}

	enrichedRecords, err := e.EnrichBatch(records, metadata)
	if err != nil {
		t.Fatalf("Failed to enrich: %v", err)
	}

	// Write to file
	outputDir := t.TempDir()
	outputFile := filepath.Join(outputDir, "output.ndjson")

	w := writer.New(writer.Config{
		Filename: outputFile,
	})

	_, err = w.WriteBatch(enrichedRecords)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Verify output
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	// Parse the output (ignoring padding spaces)
	lines := strings.Split(strings.TrimSpace(string(outputData)), "\n")
	if len(lines) != 1 {
		t.Fatalf("Expected 1 line, got %d", len(lines))
	}

	// Find the end of JSON (before padding)
	line := strings.TrimRight(lines[0], " ")
	var result map[string]any
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		t.Fatalf("Failed to parse output JSON: %v", err)
	}

	if result["message"] != "hello world" {
		t.Errorf("Unexpected message: %v", result["message"])
	}

	source, ok := result["_source"].(map[string]any)
	if !ok {
		t.Fatalf("Missing _source field")
	}

	if source["blob"] != "test/message.json" {
		t.Errorf("Unexpected blob in _source: %v", source["blob"])
	}
}

func TestE2E_DeleteAfterProcess(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test blob
	testData := []byte(`{"data":"to delete"}`)
	uploadTestBlob(t, ctx, client, "delete-me.json", testData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Verify blob exists
	blobs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("Expected 1 blob, got %d", len(blobs))
	}

	// Delete the blob
	err = r.Delete(ctx, "delete-me.json")
	if err != nil {
		t.Fatalf("Failed to delete blob: %v", err)
	}

	// Verify blob is deleted
	blobs, err = r.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs after delete: %v", err)
	}
	if len(blobs) != 0 {
		t.Errorf("Expected 0 blobs after delete, got %d", len(blobs))
	}
}

func TestE2E_FilePatternFilter(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload multiple blobs
	uploadTestBlob(t, ctx, client, "data/file1.json", []byte(`{"id":1}`))
	uploadTestBlob(t, ctx, client, "data/file2.json", []byte(`{"id":2}`))
	uploadTestBlob(t, ctx, client, "data/file3.csv", []byte(`id,name\n1,test`))
	uploadTestBlob(t, ctx, client, "logs/app.log", []byte(`log entry`))

	// Test JSON pattern
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, `.*\.json$`)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	blobs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs: %v", err)
	}

	if len(blobs) != 2 {
		t.Errorf("Expected 2 JSON blobs, got %d", len(blobs))
	}

	// Test CSV pattern
	r2, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, `.*\.csv$`)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	blobs2, err := r2.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs: %v", err)
	}

	if len(blobs2) != 1 {
		t.Errorf("Expected 1 CSV blob, got %d", len(blobs2))
	}
}

func TestE2E_ExternalCSVParser(t *testing.T) {
	skipIfNoAzurite(t)

	// Check if the CSV parser script exists
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV parser script not found at %s", scriptPath)
	}

	// Check if bash is available
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload CSV blob
	csvData := []byte(`name,age,city
Alice,30,New York
Bob,25,Los Angeles
Charlie,35,Chicago`)
	uploadTestBlob(t, ctx, client, "data/users.csv", csvData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, `.*\.csv$`)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download blob
	data, _, err := r.Download(ctx, "data/users.csv")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	// Create external parser
	parser, err := external.New(external.Config{
		ID:          "csv",
		FilePattern: `.*\.csv$`,
		Command:     "bash",
		Args:        []string{scriptPath},
		TempDir:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Failed to create external parser: %v", err)
	}

	// Parse CSV
	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse CSV: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("Expected 3 records, got %d", len(records))
	}

	// Verify first record
	if records[0]["name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", records[0]["name"])
	}
	if records[0]["age"] != "30" {
		t.Errorf("Expected age '30', got %v", records[0]["age"])
	}
	if records[0]["city"] != "New York" {
		t.Errorf("Expected city 'New York', got %v", records[0]["city"])
	}

	// Verify last record
	if records[2]["name"] != "Charlie" {
		t.Errorf("Expected name 'Charlie', got %v", records[2]["name"])
	}
}

func TestE2E_MultipleParserTypes(t *testing.T) {
	skipIfNoAzurite(t)

	// Check if the CSV parser script exists
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload both JSON and CSV blobs
	jsonData := []byte(`{"type":"json","value":42}`)
	csvData := []byte(`type,value
csv,100`)
	uploadTestBlob(t, ctx, client, "data.json", jsonData)
	uploadTestBlob(t, ctx, client, "data.csv", csvData)

	// Create reader (no filter - get all)
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Create NDJSON parser (default for .json files)
	ndjsonParser, err := ndjsonparser.New("ndjson", `.*\.json$`)
	if err != nil {
		t.Fatalf("Failed to create NDJSON parser: %v", err)
	}

	// Create CSV parser
	csvParser, err := external.New(external.Config{
		ID:          "csv",
		FilePattern: `.*\.csv$`,
		Command:     "bash",
		Args:        []string{scriptPath},
		TempDir:     t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Failed to create CSV parser: %v", err)
	}

	// List all blobs
	blobs, err := r.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list blobs: %v", err)
	}

	if len(blobs) != 2 {
		t.Fatalf("Expected 2 blobs, got %d", len(blobs))
	}

	// Process each blob with the appropriate parser
	outputDir := t.TempDir()
	outputFile := filepath.Join(outputDir, "combined.ndjson")
	w := writer.New(writer.Config{Filename: outputFile})
	defer func() { _ = w.Close() }()

	totalRecords := 0
	for _, blob := range blobs {
		data, _, err := r.Download(ctx, blob.Name)
		if err != nil {
			t.Fatalf("Failed to download %s: %v", blob.Name, err)
		}

		var records []map[string]any
		if ndjsonParser.Matches(blob.Name) {
			records, err = ndjsonParser.Parse(data)
		} else if csvParser.Matches(blob.Name) {
			records, err = csvParser.Parse(data)
		}
		_ = data.Close()

		if err != nil {
			t.Fatalf("Failed to parse %s: %v", blob.Name, err)
		}

		_, err = w.WriteBatch(records)
		if err != nil {
			t.Fatalf("Failed to write records: %v", err)
		}

		totalRecords += len(records)
	}

	if totalRecords != 2 {
		t.Errorf("Expected 2 total records, got %d", totalRecords)
	}
}

func TestE2E_LargeBlob(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Create a large NDJSON blob (1000 records)
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString(fmt.Sprintf(`{"id":%d,"data":"record number %d with some extra padding to make it larger"}`, i, i))
		sb.WriteString("\n")
	}
	largeData := []byte(sb.String())

	uploadTestBlob(t, ctx, client, "large/data.json", largeData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download and parse
	data, _, err := r.Download(ctx, "large/data.json")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	parser, err := ndjsonparser.New("ndjson", "")
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(records) != 1000 {
		t.Errorf("Expected 1000 records, got %d", len(records))
	}
}

func TestE2E_GzipOutput(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test blob
	testData := []byte(`{"message":"gzip test"}`)
	uploadTestBlob(t, ctx, client, "test.json", testData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, "")
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download and parse
	data, _, err := r.Download(ctx, "test.json")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	parser, err := ndjsonparser.New("ndjson", "")
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Write with gzip
	outputDir := t.TempDir()
	outputFile := filepath.Join(outputDir, "output.ndjson.gz")

	w := writer.New(writer.Config{
		Filename: outputFile,
		Gzip:     true,
	})

	_, err = w.WriteBatch(records)
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Verify gzip file was created
	if _, err := os.Stat(outputFile); os.IsNotExist(err) {
		t.Fatal("Gzip output file was not created")
	}

	// Verify it's a valid gzip file by reading it
	file, err := os.Open(outputFile)
	if err != nil {
		t.Fatalf("Failed to open gzip file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// Check gzip magic number
	magic := make([]byte, 2)
	_, err = file.Read(magic)
	if err != nil {
		t.Fatalf("Failed to read gzip magic: %v", err)
	}

	if magic[0] != 0x1f || magic[1] != 0x8b {
		t.Error("Output file does not have gzip magic number")
	}
}

func TestCSVParserScript(t *testing.T) {
	// Test the CSV parser script directly (file mode)
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	// Create temp directory
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "input.csv")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Write test CSV
	csvContent := `id,name,email
1,John Doe,john@example.com
2,Jane Smith,jane@example.com
3,"Bob, Jr.",bob@example.com`

	if err := os.WriteFile(inputFile, []byte(csvContent), 0644); err != nil {
		t.Fatalf("Failed to write input file: %v", err)
	}

	// Run the script
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INPUT_FILE=%s", inputFile),
		fmt.Sprintf("OUTPUT_FILE=%s", outputFile),
		fmt.Sprintf("TEMP_DIR=%s", tempDir),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Script failed: %v, output: %s", err, string(output))
	}

	// Read and parse output (NDJSON format)
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	// Parse NDJSON (one JSON object per line)
	lines := strings.Split(strings.TrimSpace(string(outputData)), "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines (NDJSON), got %d, content: %s", len(lines), string(outputData))
	}

	var records []map[string]any
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("Failed to parse NDJSON line: %v, line: %s", err, line)
		}
		records = append(records, record)
	}

	// Verify records
	if records[0]["id"] != "1" {
		t.Errorf("Expected id '1', got %v", records[0]["id"])
	}
	if records[0]["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", records[0]["name"])
	}
	if records[1]["email"] != "jane@example.com" {
		t.Errorf("Expected email 'jane@example.com', got %v", records[1]["email"])
	}
}

func TestCSVParserScript_EmptyFile(t *testing.T) {
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "empty.csv")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Write empty file
	if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write input file: %v", err)
	}

	// Run the script
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INPUT_FILE=%s", inputFile),
		fmt.Sprintf("OUTPUT_FILE=%s", outputFile),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Script failed: %v, output: %s", err, string(output))
	}

	// Read output - should be empty for NDJSON
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	outputStr := strings.TrimSpace(string(outputData))
	if outputStr != "" {
		t.Errorf("Expected empty output for empty CSV, got: %s", outputStr)
	}
}

func TestCSVParserScript_HeadersOnly(t *testing.T) {
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "headers.csv")
	outputFile := filepath.Join(tempDir, "output.ndjson")

	// Write headers only
	if err := os.WriteFile(inputFile, []byte("col1,col2,col3\n"), 0644); err != nil {
		t.Fatalf("Failed to write input file: %v", err)
	}

	// Run the script
	cmd := exec.Command("bash", scriptPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("INPUT_FILE=%s", inputFile),
		fmt.Sprintf("OUTPUT_FILE=%s", outputFile),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Script failed: %v, output: %s", err, string(output))
	}

	// Read output - should be empty for headers-only (NDJSON)
	outputData, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	outputStr := strings.TrimSpace(string(outputData))
	if outputStr != "" {
		t.Errorf("Expected empty output for headers-only CSV, got: %s", outputStr)
	}
}

func TestCSVParserScriptStdio(t *testing.T) {
	// Test the stdin/stdout CSV parser script
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json-stdio.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV stdio parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	// Test CSV content
	csvContent := `id,name,email
1,John Doe,john@example.com
2,Jane Smith,jane@example.com
3,Bob Johnson,bob@example.com`

	// Run the script with stdin
	cmd := exec.Command("bash", scriptPath)
	cmd.Stdin = strings.NewReader(csvContent)

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Script failed: %v", err)
	}

	// Parse NDJSON output (one JSON object per line)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines (NDJSON), got %d, content: %s", len(lines), string(output))
	}

	var records []map[string]any
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("Failed to parse NDJSON line: %v, line: %s", err, line)
		}
		records = append(records, record)
	}

	// Verify records
	if records[0]["id"] != "1" {
		t.Errorf("Expected id '1', got %v", records[0]["id"])
	}
	if records[0]["name"] != "John Doe" {
		t.Errorf("Expected name 'John Doe', got %v", records[0]["name"])
	}
	if records[2]["email"] != "bob@example.com" {
		t.Errorf("Expected email 'bob@example.com', got %v", records[2]["email"])
	}
}

func TestE2E_ExternalParserStdinStdout(t *testing.T) {
	skipIfNoAzurite(t)

	// Check if the stdin/stdout CSV parser script exists
	scriptPath, err := filepath.Abs("../../scripts/parsers/csv-to-json-stdio.sh")
	if err != nil {
		t.Fatalf("Failed to get script path: %v", err)
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: CSV stdio parser script not found at %s", scriptPath)
	}

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("Skipping test: bash not available")
	}

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload TSV blob (using comma as separator for testing)
	tsvData := []byte(`product_id,product_name,price
SKU001,Wireless Mouse,29.99
SKU002,USB Keyboard,49.99
SKU003,Monitor Stand,59.99`)
	uploadTestBlob(t, ctx, client, "inventory/products.tsv", tsvData)

	// Create reader
	r, err := reader.NewWithConnectionString(azuriteConnectionString, testContainerName, `.*\.tsv$`)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}

	// Download blob
	data, _, err := r.Download(ctx, "inventory/products.tsv")
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer func() { _ = data.Close() }()

	// Create external parser with stdin/stdout mode
	parser, err := external.New(external.Config{
		ID:          "csv-stdio",
		FilePattern: `.*\.tsv$`,
		Command:     "bash",
		Args:        []string{scriptPath},
		TempDir:     t.TempDir(),
		Stdin:       true,
		Stdout:      true,
	})
	if err != nil {
		t.Fatalf("Failed to create external parser: %v", err)
	}

	// Parse data
	records, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("Expected 3 records, got %d", len(records))
	}

	// Verify first record
	if records[0]["product_id"] != "SKU001" {
		t.Errorf("Expected product_id 'SKU001', got %v", records[0]["product_id"])
	}
	if records[0]["product_name"] != "Wireless Mouse" {
		t.Errorf("Expected product_name 'Wireless Mouse', got %v", records[0]["product_name"])
	}
	if records[0]["price"] != "29.99" {
		t.Errorf("Expected price '29.99', got %v", records[0]["price"])
	}
}

func TestE2E_DiskSpaceCheck(t *testing.T) {
	skipIfNoAzurite(t)

	ctx := context.Background()
	client, cleanup := setupTestContainer(t, ctx)
	defer cleanup()

	// Upload test blob
	testData := []byte(`{"message":"disk space test"}`)
	uploadTestBlob(t, ctx, client, "disk-test.json", testData)

	// Test GetFreeSpace utility
	tempDir := t.TempDir()

	freeSpace, err := getFreeSpace(tempDir)
	if err != nil {
		t.Fatalf("Failed to get free space: %v", err)
	}

	t.Logf("Free space in temp dir: %d bytes (%.2f MB)", freeSpace, float64(freeSpace)/(1024*1024))

	if freeSpace == 0 {
		t.Error("Expected non-zero free space")
	}
}

func TestE2E_ParseByteSize(t *testing.T) {
	// Test various byte size formats
	tests := []struct {
		name          string
		input         string
		expectedBytes uint64
		wantErr       bool
	}{
		{"1GB", "1GB", 1000000000, false},
		{"1GiB", "1GiB", 1073741824, false},
		{"500MB", "500MB", 500000000, false},
		{"100MiB", "100MiB", 104857600, false},
		{"256MiB", "256MiB", 268435456, false},
		{"1KB", "1KB", 1000, false},
		{"1KiB", "1KiB", 1024, false},
		{"invalid", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseByteSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if result != tt.expectedBytes {
				t.Errorf("parseByteSize(%q) = %d, want %d", tt.input, result, tt.expectedBytes)
			}
		})
	}
}

// parseByteSize is a local helper for testing (mirrors config.ParseByteSize)
func parseByteSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	re := regexp.MustCompile(`(?i)^(\d+(?:\.\d+)?)\s*(b|kb|kib|mb|mib|gb|gib|tb|tib)?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid byte size format: %s", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in byte size: %s", s)
	}

	unit := strings.ToLower(matches[2])
	var multiplier float64 = 1

	switch unit {
	case "", "b":
		multiplier = 1
	case "kb":
		multiplier = 1000
	case "kib":
		multiplier = 1024
	case "mb":
		multiplier = 1000 * 1000
	case "mib":
		multiplier = 1024 * 1024
	case "gb":
		multiplier = 1000 * 1000 * 1000
	case "gib":
		multiplier = 1024 * 1024 * 1024
	case "tb":
		multiplier = 1000 * 1000 * 1000 * 1000
	case "tib":
		multiplier = 1024 * 1024 * 1024 * 1024
	}

	return uint64(value * multiplier), nil
}

// getFreeSpace is a local helper for testing
func getFreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
