// Package ndjson provides an NDJSON (Newline Delimited JSON) parser implementation.
package ndjson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
)

// Parser parses NDJSON (Newline Delimited JSON) data from blobs.
// Each line is expected to contain a single JSON object.
// This is the default parser for .json files as it's the most common format for log data.
type Parser struct {
	*parser.BaseParser
	logger *slog.Logger
}

// New creates a new NDJSON parser.
func New(id, filePattern string) (*Parser, error) {
	if id == "" {
		id = "ndjson"
	}
	base, err := parser.NewBaseParser(id, filePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid file pattern: %w", err)
	}
	return &Parser{
		BaseParser: base,
		logger:     slog.Default(),
	}, nil
}

// SetLogger sets the logger for the parser.
func (p *Parser) SetLogger(logger *slog.Logger) {
	if logger != nil {
		p.logger = logger
	}
}

// Parse reads NDJSON data and returns parsed records.
// Each line is expected to contain a single JSON object.
// Lines that fail to parse are logged and skipped.
// Empty lines and whitespace-only lines are ignored.
func (p *Parser) Parse(input io.Reader) ([]map[string]any, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	records, _ := p.parseNDJSON(data)
	return records, nil
}

// ParseWithContext reads NDJSON data with additional context and returns parsed records.
// This is an alias for Parse as the NDJSON parser does not use context.
func (p *Parser) ParseWithContext(input io.Reader, _ parser.ParseContext) ([]map[string]any, error) {
	return p.Parse(input)
}

// parseNDJSON parses newline-delimited JSON.
// Each line is expected to contain a single JSON object.
// Lines that fail to parse are logged and skipped.
// Returns the successfully parsed records and the count of parse errors.
func (p *Parser) parseNDJSON(data []byte) ([]map[string]any, int) {
	var records []map[string]any
	parseErrors := 0

	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Increase buffer size for potentially long lines
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		// Skip empty lines and whitespace-only lines
		trimmedLine := bytes.TrimSpace(line)
		if len(trimmedLine) == 0 {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal(trimmedLine, &record); err != nil {
			parseErrors++
			// Log the error with the problematic line content
			lineStr := string(trimmedLine)
			if len(lineStr) > 200 {
				lineStr = lineStr[:200] + "..."
			}
			p.logger.Warn("failed to parse NDJSON line",
				"line_number", lineNum,
				"error", err.Error(),
				"content", lineStr,
			)
			continue
		}
		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		p.logger.Error("error reading input", "error", err)
	}

	return records, parseErrors
}
