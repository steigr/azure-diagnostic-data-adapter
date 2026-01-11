package writer

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_Write(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "test.ndjson")

	w := New(Config{
		Filename:   filename,
		MaxSize:    1,
		MaxBackups: 1,
		MaxAge:     1,
	})
	defer func() { _ = w.Close() }()

	record := map[string]any{
		"key":   "value",
		"count": 42,
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read back and verify - the first line should be padded
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// First write to new file should be at least MinFirstWriteSize bytes
	if len(data) < MinFirstWriteSize {
		t.Errorf("First write size = %d, want >= %d", len(data), MinFirstWriteSize)
	}

	// Find the JSON part (before the padding spaces)
	jsonEnd := 0
	for i, b := range data {
		if b == '}' {
			jsonEnd = i + 1
			break
		}
	}

	var readBack map[string]any
	if err := json.Unmarshal(data[:jsonEnd], &readBack); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if readBack["key"] != "value" {
		t.Errorf("readBack[key] = %v, want value", readBack["key"])
	}
}

func TestWriter_Write_FirstWritePadding(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "padding.ndjson")

	w := New(Config{
		Filename: filename,
	})
	defer func() { _ = w.Close() }()

	// Write a small record
	record := map[string]any{"a": 1}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read back and check size
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// First write should be padded to MinFirstWriteSize
	if len(data) < MinFirstWriteSize {
		t.Errorf("First write size = %d, want >= %d", len(data), MinFirstWriteSize)
	}

	// Should end with spaces and newline
	if data[len(data)-1] != '\n' {
		t.Error("Data should end with newline")
	}

	// Write a second record - should NOT be padded
	record2 := map[string]any{"b": 2}
	if err := w.Write(record2); err != nil {
		t.Fatalf("Write() second record error = %v", err)
	}

	data2, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Second write should add just the record size (not padded)
	secondWriteSize := len(data2) - len(data)
	if secondWriteSize >= MinFirstWriteSize {
		t.Errorf("Second write was padded, size = %d", secondWriteSize)
	}
}

func TestWriter_Write_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "existing.ndjson")

	// Create a file with existing content
	existingContent := []byte(`{"existing": true}` + "\n")
	if err := os.WriteFile(filename, existingContent, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	w := New(Config{
		Filename: filename,
	})
	defer func() { _ = w.Close() }()

	// Write a small record to existing file - should NOT be padded
	record := map[string]any{"new": true}
	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// The new write should NOT be padded since file already existed
	expectedMinSize := len(existingContent) + len(`{"new":true}`) + 1 // +1 for newline
	if len(data) >= len(existingContent)+MinFirstWriteSize {
		t.Errorf("Write to existing file was padded, total size = %d, expected around %d", len(data), expectedMinSize)
	}
}

func TestWriter_Write_LargeFirstWrite(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "large.ndjson")

	w := New(Config{
		Filename: filename,
	})
	defer func() { _ = w.Close() }()

	// Create a record larger than MinFirstWriteSize
	largeValue := make([]byte, MinFirstWriteSize+100)
	for i := range largeValue {
		largeValue[i] = 'x'
	}
	record := map[string]any{"large": string(largeValue)}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Large record should NOT have extra padding
	// Just verify it ends with newline and contains no trailing spaces before newline
	if data[len(data)-1] != '\n' {
		t.Error("Data should end with newline")
	}

	// The JSON should be parseable without stripping spaces
	var readBack map[string]any
	if err := json.Unmarshal(data[:len(data)-1], &readBack); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
}

func TestWriter_WriteBatch(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "batch.ndjson")

	w := New(Config{
		Filename: filename,
	})
	defer func() { _ = w.Close() }()

	records := []map[string]any{
		{"id": 1},
		{"id": 2},
		{"id": 3},
	}

	written, err := w.WriteBatch(records)
	if err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}

	if written != 3 {
		t.Errorf("WriteBatch() wrote %d, want 3", written)
	}

	// Verify file has 3 lines
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
	}

	if lines != 3 {
		t.Errorf("File has %d lines, want 3", lines)
	}
}

func TestWriter_Close(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "close.ndjson")

	w := New(Config{
		Filename: filename,
	})

	record := map[string]any{"test": true}
	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("File was not created")
	}
}

func TestWriter_Gzip(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "test.ndjson.gz")

	w := New(Config{
		Filename: filename,
		Gzip:     true,
	})

	record := map[string]any{
		"key":   "value",
		"count": 42,
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read and decompress the file
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() { _ = gz.Close() }()

	scanner := bufio.NewScanner(gz)
	if !scanner.Scan() {
		t.Fatal("Expected to read a line from gzip file")
	}

	var readBack map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &readBack); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if readBack["key"] != "value" {
		t.Errorf("readBack[key] = %v, want value", readBack["key"])
	}
}

func TestWriter_Gzip_NoPadding(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "nopad.ndjson.gz")

	w := New(Config{
		Filename: filename,
		Gzip:     true,
	})

	// Write a small record - should NOT be padded with gzip
	record := map[string]any{"a": 1}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read and decompress the file
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() { _ = gz.Close() }()

	scanner := bufio.NewScanner(gz)
	if !scanner.Scan() {
		t.Fatal("Expected to read a line from gzip file")
	}

	line := scanner.Text()
	// Gzip output should NOT have padding - line should be short
	if len(line) >= MinFirstWriteSize {
		t.Errorf("Gzip output was padded, line length = %d", len(line))
	}

	// Verify it's valid JSON
	var readBack map[string]any
	if err := json.Unmarshal([]byte(line), &readBack); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
}

func TestWriter_Gzip_WriteBatch(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "batch.ndjson.gz")

	w := New(Config{
		Filename: filename,
		Gzip:     true,
	})

	records := []map[string]any{
		{"id": 1},
		{"id": 2},
		{"id": 3},
	}

	written, err := w.WriteBatch(records)
	if err != nil {
		t.Fatalf("WriteBatch() error = %v", err)
	}

	if written != 3 {
		t.Errorf("WriteBatch() wrote %d, want 3", written)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read and decompress the file
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() { _ = gz.Close() }()

	scanner := bufio.NewScanner(gz)
	lines := 0
	for scanner.Scan() {
		lines++
	}

	if lines != 3 {
		t.Errorf("Gzip file has %d lines, want 3", lines)
	}
}
