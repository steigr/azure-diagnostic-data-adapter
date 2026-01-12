package writer

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestGenerateSourceHash(t *testing.T) {
	tests := []struct {
		name     string
		info     SourceInfo
		wantLen  int
		wantSame bool
		other    SourceInfo
	}{
		{
			name: "generates 8 char hash",
			info: SourceInfo{
				BlobName:           "logs/2026/01/12/data.json",
				ContainerName:      "my-container",
				StorageAccountName: "mystorageaccount",
			},
			wantLen: 8,
		},
		{
			name: "same input produces same hash",
			info: SourceInfo{
				BlobName:           "test.json",
				ContainerName:      "container",
				StorageAccountName: "account",
			},
			wantSame: true,
			other: SourceInfo{
				BlobName:           "test.json",
				ContainerName:      "container",
				StorageAccountName: "account",
			},
		},
		{
			name: "different blob produces different hash",
			info: SourceInfo{
				BlobName:           "test1.json",
				ContainerName:      "container",
				StorageAccountName: "account",
			},
			wantSame: false,
			other: SourceInfo{
				BlobName:           "test2.json",
				ContainerName:      "container",
				StorageAccountName: "account",
			},
		},
		{
			name: "different container produces different hash",
			info: SourceInfo{
				BlobName:           "test.json",
				ContainerName:      "container1",
				StorageAccountName: "account",
			},
			wantSame: false,
			other: SourceInfo{
				BlobName:           "test.json",
				ContainerName:      "container2",
				StorageAccountName: "account",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := GenerateSourceHash(tt.info)

			if tt.wantLen > 0 && len(hash) != tt.wantLen {
				t.Errorf("GenerateSourceHash() len = %d, want %d", len(hash), tt.wantLen)
			}

			if tt.other.BlobName != "" || tt.other.ContainerName != "" {
				otherHash := GenerateSourceHash(tt.other)
				if tt.wantSame && hash != otherHash {
					t.Errorf("Expected same hash, got %s and %s", hash, otherHash)
				}
				if !tt.wantSame && hash == otherHash {
					t.Errorf("Expected different hash, both got %s", hash)
				}
			}
		})
	}
}

func TestMultiWriter_WriteBatchWithSource(t *testing.T) {
	dir := t.TempDir()
	baseFilename := filepath.Join(dir, "output.ndjson")

	mw := NewMultiWriter(Config{
		Filename:   baseFilename,
		MaxSize:    100,
		MaxBackups: 3,
		MaxAge:     7,
	})
	defer func() { _ = mw.Close() }()

	// Write to two different sources
	source1 := SourceInfo{
		BlobName:           "logs/file1.json",
		ContainerName:      "container1",
		StorageAccountName: "account1",
	}
	source2 := SourceInfo{
		BlobName:           "logs/file2.json",
		ContainerName:      "container2",
		StorageAccountName: "account2",
	}

	records1 := []map[string]any{
		{"source": "1", "id": 1},
		{"source": "1", "id": 2},
	}
	records2 := []map[string]any{
		{"source": "2", "id": 1},
	}

	written1, err := mw.WriteBatchWithSource(records1, source1)
	if err != nil {
		t.Fatalf("WriteBatchWithSource(source1) error = %v", err)
	}
	if written1 != 2 {
		t.Errorf("written1 = %d, want 2", written1)
	}

	written2, err := mw.WriteBatchWithSource(records2, source2)
	if err != nil {
		t.Fatalf("WriteBatchWithSource(source2) error = %v", err)
	}
	if written2 != 1 {
		t.Errorf("written2 = %d, want 1", written2)
	}

	// Should have 2 writers
	if mw.GetWriterCount() != 2 {
		t.Errorf("GetWriterCount() = %d, want 2", mw.GetWriterCount())
	}

	// Verify files were created with hash suffixes
	hash1 := GenerateSourceHash(source1)
	hash2 := GenerateSourceHash(source2)

	file1 := filepath.Join(dir, "output_"+hash1+".ndjson")
	file2 := filepath.Join(dir, "output_"+hash2+".ndjson")

	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Errorf("Expected file %s to exist", file1)
	}
	if _, err := os.Stat(file2); os.IsNotExist(err) {
		t.Errorf("Expected file %s to exist", file2)
	}
}

func TestMultiWriter_SameSourceSameFile(t *testing.T) {
	dir := t.TempDir()
	baseFilename := filepath.Join(dir, "output.ndjson")

	mw := NewMultiWriter(Config{
		Filename: baseFilename,
	})
	defer func() { _ = mw.Close() }()

	source := SourceInfo{
		BlobName:           "test.json",
		ContainerName:      "container",
		StorageAccountName: "account",
	}

	// Write multiple batches to same source
	for i := 0; i < 3; i++ {
		records := []map[string]any{{"batch": i}}
		if _, err := mw.WriteBatchWithSource(records, source); err != nil {
			t.Fatalf("WriteBatchWithSource() error = %v", err)
		}
	}

	// Should still have only 1 writer
	if mw.GetWriterCount() != 1 {
		t.Errorf("GetWriterCount() = %d, want 1", mw.GetWriterCount())
	}

	// Verify file has 3 lines
	hash := GenerateSourceHash(source)
	filename := filepath.Join(dir, "output_"+hash+".ndjson")

	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("File has %d lines, want 3", lines)
	}
}

func TestMultiWriter_DeleteDelay(t *testing.T) {
	dir := t.TempDir()
	baseFilename := filepath.Join(dir, "output.ndjson")

	// Use a short delete delay for testing
	mw := NewMultiWriter(Config{
		Filename:    baseFilename,
		DeleteDelay: 100 * time.Millisecond,
	})
	defer func() { _ = mw.Close() }()

	source := SourceInfo{
		BlobName:           "test.json",
		ContainerName:      "container",
		StorageAccountName: "account",
	}

	records := []map[string]any{{"test": true}}
	if _, err := mw.WriteBatchWithSource(records, source); err != nil {
		t.Fatalf("WriteBatchWithSource() error = %v", err)
	}

	// File should exist immediately
	hash := GenerateSourceHash(source)
	filename := filepath.Join(dir, "output_"+hash+".ndjson")

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Fatalf("File should exist immediately after write")
	}

	// Should have 1 pending deletion
	if mw.GetPendingDeletionCount() != 1 {
		t.Errorf("GetPendingDeletionCount() = %d, want 1", mw.GetPendingDeletionCount())
	}

	// Wait for deletion
	time.Sleep(200 * time.Millisecond)

	// File should be deleted
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Errorf("File should be deleted after delay")
	}

	// No more pending deletions
	if mw.GetPendingDeletionCount() != 0 {
		t.Errorf("GetPendingDeletionCount() = %d, want 0", mw.GetPendingDeletionCount())
	}
}

func TestMultiWriter_DeleteDelayDisabled(t *testing.T) {
	dir := t.TempDir()
	baseFilename := filepath.Join(dir, "output.ndjson")

	// DeleteDelay = 0 should disable deletion
	mw := NewMultiWriter(Config{
		Filename:    baseFilename,
		DeleteDelay: 0,
	})
	defer func() { _ = mw.Close() }()

	source := SourceInfo{
		BlobName:           "test.json",
		ContainerName:      "container",
		StorageAccountName: "account",
	}

	records := []map[string]any{{"test": true}}
	if _, err := mw.WriteBatchWithSource(records, source); err != nil {
		t.Fatalf("WriteBatchWithSource() error = %v", err)
	}

	// No pending deletions when disabled
	if mw.GetPendingDeletionCount() != 0 {
		t.Errorf("GetPendingDeletionCount() = %d, want 0 when disabled", mw.GetPendingDeletionCount())
	}

	// File should still exist
	hash := GenerateSourceHash(source)
	filename := filepath.Join(dir, "output_"+hash+".ndjson")

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Fatalf("File should exist")
	}
}

func TestMultiWriter_DeleteDelayReset(t *testing.T) {
	dir := t.TempDir()
	baseFilename := filepath.Join(dir, "output.ndjson")

	mw := NewMultiWriter(Config{
		Filename:    baseFilename,
		DeleteDelay: 150 * time.Millisecond,
	})
	defer func() { _ = mw.Close() }()

	source := SourceInfo{
		BlobName:           "test.json",
		ContainerName:      "container",
		StorageAccountName: "account",
	}

	// First write
	records := []map[string]any{{"batch": 1}}
	if _, err := mw.WriteBatchWithSource(records, source); err != nil {
		t.Fatalf("WriteBatchWithSource() error = %v", err)
	}

	// Wait 100ms and write again - this should reset the timer
	time.Sleep(100 * time.Millisecond)

	records = []map[string]any{{"batch": 2}}
	if _, err := mw.WriteBatchWithSource(records, source); err != nil {
		t.Fatalf("WriteBatchWithSource() error = %v", err)
	}

	// File should still exist after 100ms (timer was reset)
	time.Sleep(100 * time.Millisecond)

	hash := GenerateSourceHash(source)
	filename := filepath.Join(dir, "output_"+hash+".ndjson")

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Errorf("File should still exist - timer should have been reset")
	}

	// Wait for deletion after reset
	time.Sleep(100 * time.Millisecond)

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Errorf("File should be deleted after delay from last write")
	}
}
