// Package parser provides interfaces and types for parsing blob data.
package parser

import (
	"io"
	"regexp"
)

// ParseContext provides additional context for parsing, including blob metadata
// and storage information that can be used for enrichment or external processes.
type ParseContext struct {
	// BlobName is the name of the blob being parsed
	BlobName string
	// ContainerName is the name of the container the blob is in
	ContainerName string
	// StorageAccountName is the name of the storage account
	StorageAccountName string
	// FileMatch contains named capture groups from the file_pattern regex
	FileMatch map[string]string
}

// Parser defines the interface for parsing blob data.
type Parser interface {
	// Parse reads data from the input and returns parsed records.
	Parse(input io.Reader) ([]map[string]any, error)

	// ParseWithContext reads data from the input with additional context and returns parsed records.
	// Implementations should use this for external parsers that need storage information.
	ParseWithContext(input io.Reader, ctx ParseContext) ([]map[string]any, error)

	// ID returns the unique identifier for this parser.
	ID() string

	// Matches checks if this parser should be used for the given blob name.
	Matches(blobName string) bool

	// MatchResult extracts named capture groups from the blob name.
	// Returns a map of capture group names to their matched values.
	MatchResult(blobName string) map[string]string

	// DataGrowthFactor returns the estimated ratio of output size to input size.
	// For example, a factor of 3.0 means the output is expected to be 3 times larger than the input.
	// Default is 1.0 if not configured.
	DataGrowthFactor() float64
}

// Config holds the configuration for a parser.
type Config struct {
	ID               string            `mapstructure:"id"`
	Type             string            `mapstructure:"type"` // "json" or "external"
	FilePattern      string            `mapstructure:"file_pattern"`
	Command          string            `mapstructure:"command"`
	Args             []string          `mapstructure:"args"`
	Env              map[string]string `mapstructure:"env"`
	Stdin            bool              `mapstructure:"stdin"`              // For external: read input from stdin
	Stdout           bool              `mapstructure:"stdout"`             // For external: write output to stdout
	DataGrowthFactor float64           `mapstructure:"data_growth_factor"` // Estimated output/input size ratio (default: 1.0)
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
	id               string
	pattern          *regexp.Regexp
	dataGrowthFactor float64
}

// NewBaseParser creates a new BaseParser with the given ID and file pattern.
// The file pattern matching is case-insensitive.
func NewBaseParser(id, filePattern string) (*BaseParser, error) {
	return NewBaseParserWithGrowthFactor(id, filePattern, 1.0)
}

// NewBaseParserWithGrowthFactor creates a new BaseParser with the given ID, file pattern, and data growth factor.
// The file pattern matching is case-insensitive.
func NewBaseParserWithGrowthFactor(id, filePattern string, dataGrowthFactor float64) (*BaseParser, error) {
	var pattern *regexp.Regexp
	var err error
	if filePattern != "" {
		// Prepend (?i) to make the pattern case-insensitive
		pattern, err = regexp.Compile("(?i)" + filePattern)
		if err != nil {
			return nil, err
		}
	}
	// Default to 1.0 if not set or invalid
	if dataGrowthFactor <= 0 {
		dataGrowthFactor = 1.0
	}
	return &BaseParser{
		id:               id,
		pattern:          pattern,
		dataGrowthFactor: dataGrowthFactor,
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

// MatchResult extracts named capture groups from the blob name using the file pattern.
// Returns a map of capture group names to their matched values.
// If no pattern is configured or the pattern doesn't match, returns an empty map.
func (b *BaseParser) MatchResult(blobName string) map[string]string {
	result := make(map[string]string)
	if b.pattern == nil {
		return result
	}

	match := b.pattern.FindStringSubmatch(blobName)
	if match == nil {
		return result
	}

	names := b.pattern.SubexpNames()
	for i, name := range names {
		if i > 0 && name != "" && i < len(match) {
			result[name] = match[i]
		}
	}

	return result
}

// DataGrowthFactor returns the estimated ratio of output size to input size.
func (b *BaseParser) DataGrowthFactor() float64 {
	if b.dataGrowthFactor <= 0 {
		return 1.0
	}
	return b.dataGrowthFactor
}

// ParseWithContext is a default implementation that calls Parse without using context.
// Subclasses that need context (like external parsers) should override this method.
func (b *BaseParser) ParseWithContext(_ io.Reader, _ ParseContext) ([]map[string]any, error) {
	// Default implementation ignores context and calls the simple Parse method.
	// This should be overridden by parsers that need the context.
	return nil, nil
}
