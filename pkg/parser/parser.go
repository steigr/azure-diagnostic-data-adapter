// Package parser provides interfaces and types for parsing blob data.
package parser

import (
	"io"
	"regexp"
)

// Parser defines the interface for parsing blob data.
type Parser interface {
	// Parse reads data from the input and returns parsed records.
	Parse(input io.Reader) ([]map[string]any, error)

	// ID returns the unique identifier for this parser.
	ID() string

	// Matches checks if this parser should be used for the given blob name.
	Matches(blobName string) bool
}

// Config holds the configuration for a parser.
type Config struct {
	ID          string            `mapstructure:"id"`
	Type        string            `mapstructure:"type"` // "json" or "external"
	FilePattern string            `mapstructure:"file_pattern"`
	Command     string            `mapstructure:"command"`
	Args        []string          `mapstructure:"args"`
	Env         map[string]string `mapstructure:"env"`
	Stdin       bool              `mapstructure:"stdin"`  // For external: read input from stdin
	Stdout      bool              `mapstructure:"stdout"` // For external: write output to stdout
}

// Registry holds registered parsers.
type Registry struct {
	parsers []Parser
}

// NewRegistry creates a new parser registry.
func NewRegistry() *Registry {
	return &Registry{
		parsers: make([]Parser, 0),
	}
}

// Register adds a parser to the registry.
func (r *Registry) Register(p Parser) {
	r.parsers = append(r.parsers, p)
}

// Get retrieves a parser by ID.
func (r *Registry) Get(id string) (Parser, bool) {
	for _, p := range r.parsers {
		if p.ID() == id {
			return p, true
		}
	}
	return nil, false
}

// FindMatching finds the first parser that matches the given blob name.
func (r *Registry) FindMatching(blobName string) (Parser, bool) {
	for _, p := range r.parsers {
		if p.Matches(blobName) {
			return p, true
		}
	}
	return nil, false
}

// List returns all registered parser IDs.
func (r *Registry) List() []string {
	ids := make([]string, 0, len(r.parsers))
	for _, p := range r.parsers {
		ids = append(ids, p.ID())
	}
	return ids
}

// BaseParser provides common functionality for parsers.
type BaseParser struct {
	id      string
	pattern *regexp.Regexp
}

// NewBaseParser creates a new BaseParser with the given ID and file pattern.
// The file pattern matching is case-insensitive.
func NewBaseParser(id, filePattern string) (*BaseParser, error) {
	var pattern *regexp.Regexp
	var err error
	if filePattern != "" {
		// Prepend (?i) to make the pattern case-insensitive
		pattern, err = regexp.Compile("(?i)" + filePattern)
		if err != nil {
			return nil, err
		}
	}
	return &BaseParser{
		id:      id,
		pattern: pattern,
	}, nil
}

// ID returns the parser identifier.
func (b *BaseParser) ID() string {
	return b.id
}

// Matches checks if the blob name matches the parser's file pattern.
// If no pattern is configured, it matches all files.
func (b *BaseParser) Matches(blobName string) bool {
	if b.pattern == nil {
		return true
	}
	return b.pattern.MatchString(blobName)
}
