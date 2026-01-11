// Package writer provides NDJSON file writing with log rotation.
package writer

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

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
}

// New creates a new Writer with the given configuration.
func New(cfg Config) *Writer {
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 100 // 100 MB default
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 3
	}
	if cfg.MaxAge == 0 {
		cfg.MaxAge = 28 // 28 days default
	}

	logger := &lumberjack.Logger{
		Filename:   cfg.Filename,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
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
