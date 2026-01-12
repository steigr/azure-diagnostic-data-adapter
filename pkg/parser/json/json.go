// Package json provides a JSON parser implementation for JSON arrays and single objects.
package json

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
)

// Parser parses JSON array or single object data from blobs.
// This parser is for structured JSON (arrays or single objects), not for NDJSON.
// For NDJSON (newline-delimited JSON), use the ndjson parser instead.
type Parser struct {
	*parser.BaseParser
	logger *slog.Logger
}

// New creates a new JSON parser.
func New(id, filePattern string) (*Parser, error) {
	return NewWithGrowthFactor(id, filePattern, 1.0)
}

// NewWithGrowthFactor creates a new JSON parser with a specified data growth factor.
func NewWithGrowthFactor(id, filePattern string, dataGrowthFactor float64) (*Parser, error) {
	if id == "" {
		id = "json"
	}
	base, err := parser.NewBaseParserWithGrowthFactor(id, filePattern, dataGrowthFactor)
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

// Parse reads JSON data and returns parsed records.
// Supports JSON arrays (returns all elements) and single JSON objects (returns one record).
// For NDJSON (newline-delimited JSON), use the ndjson parser instead.
func (p *Parser) Parse(input io.Reader) ([]map[string]any, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if len(data) == 0 {
		return nil, nil
	}

	// Try to parse as JSON array first
	var arrayRecords []map[string]any
	if err := json.Unmarshal(data, &arrayRecords); err == nil {
		return arrayRecords, nil
	}

	// Try to parse as a single JSON object
	var single map[string]any
	if err := json.Unmarshal(data, &single); err == nil {
		return []map[string]any{single}, nil
	}

	// If neither works, return error
	return nil, fmt.Errorf("failed to parse JSON: input is neither a JSON array nor a JSON object")
}

// ParseWithContext reads JSON data with additional context and returns parsed records.
// This is an alias for Parse as the JSON parser does not use context.
func (p *Parser) ParseWithContext(input io.Reader, _ parser.ParseContext) ([]map[string]any, error) {
	return p.Parse(input)
}
