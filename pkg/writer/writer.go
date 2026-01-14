// Package writer provides NDJSON file writing with log rotation.
package writer

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// MinFirstWriteSize is the minimum size for the first write to a new file.
	// If the first write is smaller, spaces are added as padding.
	// This only applies to non-gzip output.
	MinFirstWriteSize = 1025
)

// Writer writes records as NDJSON with log rotation support.
type Writer struct {
	logger       *lumberjack.Logger
	gzipWriter   *gzip.Writer
	mu           sync.Mutex
	filename     string
	isFirstWrite bool
	gzipEnabled  bool
}

// Config holds the configuration for the writer.
type Config struct {
	// Filename is the file to write to.
	Filename string `mapstructure:"filename"`
	// MaxSize is the maximum size in megabytes of the log file before rotation.
	MaxSize int `mapstructure:"max_size"`
	// MaxBackups is the maximum number of old log files to retain.
	MaxBackups int `mapstructure:"max_backups"`
	// MaxAge is the maximum number of days to retain old log files.
	MaxAge int `mapstructure:"max_age"`
	// Gzip enables gzip compression for output.
	Gzip bool `mapstructure:"gzip"`
	// DeleteDelay is the delay before deleting output files (0 = disabled).
	DeleteDelay time.Duration `mapstructure:"delete_delay"`
}

// New creates a new Writer with the given configuration.
// If MaxSize is 0, file rotation is disabled (files grow indefinitely).
// If MaxBackups is 0, old files are not removed based on count.
// If MaxAge is 0, old files are not removed based on age.
func New(cfg Config) *Writer {
	logger := &lumberjack.Logger{
		Filename:   cfg.Filename,
		MaxSize:    cfg.MaxSize,    // 0 means no size-based rotation
		MaxBackups: cfg.MaxBackups, // 0 means keep all old files
		MaxAge:     cfg.MaxAge,     // 0 means don't remove old files based on age
	}

	// Check if the file already exists and has content (only relevant for non-gzip)
	isFirstWrite := true
	if !cfg.Gzip {
		if info, err := os.Stat(cfg.Filename); err == nil && info.Size() > 0 {
			isFirstWrite = false
		}
	}

	w := &Writer{
		logger:       logger,
		filename:     cfg.Filename,
		isFirstWrite: isFirstWrite,
		gzipEnabled:  cfg.Gzip,
	}

	// Create gzip writer if enabled
	if cfg.Gzip {
		w.gzipWriter = gzip.NewWriter(logger)
	}

	return w
}

// Write writes a single record as a JSON line.
func (w *Writer) Write(record map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Append newline
	data = append(data, '\n')

	// If this is the first write to a new file and data is below MinFirstWriteSize,
	// add space padding before the newline (only for non-gzip output)
	if !w.gzipEnabled && w.isFirstWrite && len(data) < MinFirstWriteSize {
		paddingSize := MinFirstWriteSize - len(data)
		// Insert padding (spaces) before the final newline
		paddedData := make([]byte, 0, MinFirstWriteSize)
		paddedData = append(paddedData, data[:len(data)-1]...) // JSON without newline
		for i := 0; i < paddingSize; i++ {
			paddedData = append(paddedData, ' ')
		}
		paddedData = append(paddedData, '\n')
		data = paddedData
	}

	// Write to appropriate destination
	if w.gzipEnabled {
		_, err = w.gzipWriter.Write(data)
	} else {
		_, err = w.logger.Write(data)
	}
	if err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	// After first successful write, mark as no longer first write
	if w.isFirstWrite {
		w.isFirstWrite = false
	}

	return nil
}

// WriteBatch writes multiple records as JSON lines.
func (w *Writer) WriteBatch(records []map[string]any) (int, error) {
	written := 0
	for _, record := range records {
		if err := w.Write(record); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// Flush forces any buffered data to be written.
func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.gzipEnabled && w.gzipWriter != nil {
		if err := w.gzipWriter.Flush(); err != nil {
			return fmt.Errorf("failed to flush gzip writer: %w", err)
		}
	}
	return nil
}

// Rotate forces a log rotation.
func (w *Writer) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Close and recreate gzip writer on rotation
	if w.gzipEnabled && w.gzipWriter != nil {
		if err := w.gzipWriter.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer for rotation: %w", err)
		}
	}

	if err := w.logger.Rotate(); err != nil {
		return err
	}

	// Recreate gzip writer after rotation
	if w.gzipEnabled {
		w.gzipWriter = gzip.NewWriter(w.logger)
	}

	return nil
}

// Close closes the writer.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Close gzip writer first if enabled
	if w.gzipEnabled && w.gzipWriter != nil {
		if err := w.gzipWriter.Close(); err != nil {
			return fmt.Errorf("failed to close gzip writer: %w", err)
		}
	}

	return w.logger.Close()
}

// WriterInterface defines the interface for writers.
type WriterInterface interface {
	Write(record map[string]any) error
	WriteBatch(records []map[string]any) (int, error)
	Flush() error
	Rotate() error
	io.Closer
}

// Ensure Writer implements WriterInterface
var _ WriterInterface = (*Writer)(nil)

// SourceInfo contains information about the source of data for generating output filenames.
type SourceInfo struct {
	BlobName           string
	ContainerName      string
	StorageAccountName string
}

// pendingDeletion tracks a file scheduled for deletion.
type pendingDeletion struct {
	filename string
	deleteAt time.Time
	timer    *time.Timer
}

// MultiWriter manages multiple output files based on source hash.
// Each unique combination of blob name, container name, and storage account name
// gets its own output file with a hash suffix.
type MultiWriter struct {
	baseConfig       Config
	writers          map[string]*Writer
	pendingDeletions map[string]*pendingDeletion
	mu               sync.RWMutex
	logger           *slog.Logger
}

// NewMultiWriter creates a new MultiWriter with the given base configuration.
func NewMultiWriter(cfg Config) *MultiWriter {
	return &MultiWriter{
		baseConfig:       cfg,
		writers:          make(map[string]*Writer),
		pendingDeletions: make(map[string]*pendingDeletion),
		logger:           slog.Default(),
	}
}

// SetLogger sets the logger for the MultiWriter.
func (m *MultiWriter) SetLogger(logger *slog.Logger) {
	m.logger = logger
}

// GenerateSourceHash generates a short hash from source information.
// The hash is based on blob name, container name, and storage account name.
func GenerateSourceHash(info SourceInfo) string {
	data := fmt.Sprintf("%s|%s|%s", info.StorageAccountName, info.ContainerName, info.BlobName)
	hash := sha256.Sum256([]byte(data))
	// Use first 8 characters of hex-encoded hash for a short but unique suffix
	return hex.EncodeToString(hash[:])[:8]
}

// getOrCreateWriter gets an existing writer or creates a new one for the given source.
func (m *MultiWriter) getOrCreateWriter(info SourceInfo) (*Writer, error) {
	hash := GenerateSourceHash(info)

	// Check if writer already exists (read lock)
	m.mu.RLock()
	if w, exists := m.writers[hash]; exists {
		m.mu.RUnlock()
		return w, nil
	}
	m.mu.RUnlock()

	// Create new writer (write lock)
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if w, exists := m.writers[hash]; exists {
		return w, nil
	}

	// Generate filename with hash suffix
	filename := m.generateFilename(hash)

	// Create writer config for this source
	cfg := Config{
		Filename:   filename,
		MaxSize:    m.baseConfig.MaxSize,
		MaxBackups: m.baseConfig.MaxBackups,
		MaxAge:     m.baseConfig.MaxAge,
		Gzip:       m.baseConfig.Gzip,
	}

	w := New(cfg)
	m.writers[hash] = w
	return w, nil
}

// generateFilename generates a filename with the hash suffix.
// For example: "/var/log/output.ndjson" -> "/var/log/output_abc12345.ndjson"
func (m *MultiWriter) generateFilename(hash string) string {
	base := m.baseConfig.Filename
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	return fmt.Sprintf("%s_%s%s", nameWithoutExt, hash, ext)
}

// WriteWithSource writes a single record to the appropriate file based on source info.
func (m *MultiWriter) WriteWithSource(record map[string]any, info SourceInfo) error {
	w, err := m.getOrCreateWriter(info)
	if err != nil {
		return err
	}
	if err := w.Write(record); err != nil {
		return err
	}
	// Schedule deletion if configured
	m.scheduleDelete(info)
	return nil
}

// WriteBatchWithSource writes multiple records to the appropriate file based on source info.
func (m *MultiWriter) WriteBatchWithSource(records []map[string]any, info SourceInfo) (int, error) {
	w, err := m.getOrCreateWriter(info)
	if err != nil {
		return 0, err
	}
	written, err := w.WriteBatch(records)
	if err != nil {
		return written, err
	}
	// Schedule deletion if configured
	m.scheduleDelete(info)
	return written, nil
}

// scheduleDelete schedules a file for deletion after the configured delay.
// If a deletion is already pending for this file, it resets the timer.
func (m *MultiWriter) scheduleDelete(info SourceInfo) {
	if m.baseConfig.DeleteDelay <= 0 {
		return
	}

	hash := GenerateSourceHash(info)
	filename := m.generateFilename(hash)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel existing timer if any
	if pending, exists := m.pendingDeletions[hash]; exists {
		pending.timer.Stop()
	}

	// Log the scheduled deletion
	delaySeconds := int(m.baseConfig.DeleteDelay.Seconds())
	m.logger.Debug("giving the log collector time to find and open the file",
		"filename", filename,
		"delay_seconds", delaySeconds,
	)

	// Schedule new deletion
	deleteAt := time.Now().Add(m.baseConfig.DeleteDelay)
	timer := time.AfterFunc(m.baseConfig.DeleteDelay, func() {
		m.executeDelete(hash, filename)
	})

	m.pendingDeletions[hash] = &pendingDeletion{
		filename: filename,
		deleteAt: deleteAt,
		timer:    timer,
	}
}

// executeDelete performs the actual file deletion.
func (m *MultiWriter) executeDelete(hash, filename string) {
	m.mu.Lock()

	// Close and remove the writer first
	if w, exists := m.writers[hash]; exists {
		_ = w.Close()
		delete(m.writers, hash)
	}

	// Remove from pending deletions
	delete(m.pendingDeletions, hash)

	m.mu.Unlock()

	// Delete the file
	if err := os.Remove(filename); err != nil {
		if !os.IsNotExist(err) {
			m.logger.Error("failed to delete output file",
				"filename", filename,
				"hash", hash,
				"error", err,
			)
		}
	} else {
		m.logger.Debug("deleted output file after delay",
			"filename", filename,
			"hash", hash,
		)
	}
}

// Write writes a single record (uses default writer - for backwards compatibility).
func (m *MultiWriter) Write(record map[string]any) error {
	return m.WriteWithSource(record, SourceInfo{})
}

// WriteBatch writes multiple records (uses default writer - for backwards compatibility).
func (m *MultiWriter) WriteBatch(records []map[string]any) (int, error) {
	return m.WriteBatchWithSource(records, SourceInfo{})
}

// Flush flushes all writers.
func (m *MultiWriter) Flush() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastErr error
	for _, w := range m.writers {
		if err := w.Flush(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Rotate rotates all writers.
func (m *MultiWriter) Rotate() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastErr error
	for _, w := range m.writers {
		if err := w.Rotate(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// Close closes all writers and cancels pending deletions.
func (m *MultiWriter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel all pending deletions
	for hash, pending := range m.pendingDeletions {
		pending.timer.Stop()
		delete(m.pendingDeletions, hash)
	}

	var lastErr error
	for hash, w := range m.writers {
		if err := w.Close(); err != nil {
			lastErr = err
		}
		delete(m.writers, hash)
	}
	return lastErr
}

// GetWriterCount returns the number of active writers.
func (m *MultiWriter) GetWriterCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.writers)
}

// GetPendingDeletionCount returns the number of files pending deletion.
func (m *MultiWriter) GetPendingDeletionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pendingDeletions)
}

// Ensure MultiWriter implements WriterInterface
var _ WriterInterface = (*MultiWriter)(nil)
