// Package config provides configuration handling for the Azure Diagnostic Data Adapter.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/writer"
)

// Config holds the complete application configuration.
type Config struct {
	// Source configuration (single source, optional if sources is provided)
	Source SourceConfig `mapstructure:"source"`

	// Sources configuration (multiple sources, optional if source is provided)
	Sources []SourceConfig `mapstructure:"sources"`

	// Output configuration
	Output OutputConfig `mapstructure:"output"`

	// Parsers configuration
	Parsers []parser.Config `mapstructure:"parsers"`

	// Enrichment template (Go text/template that produces JSON)
	EnrichmentTemplate string `mapstructure:"enrichment_template"`

	// Metrics configuration
	Metrics MetricsConfig `mapstructure:"metrics"`

	// Processing configuration
	Processing ProcessingConfig `mapstructure:"processing"`

	// Logging configuration
	Logging LoggingConfig `mapstructure:"logging"`

	// aggregatedSources is the internal merged and aggregated list of sources
	aggregatedSources []AggregatedSource
}

// SourceConfig holds Azure Storage Account configuration for a single source.
type SourceConfig struct {
	StorageAccountName string `mapstructure:"storage_account_name"`
	ContainerName      string `mapstructure:"container_name"`
	FilePattern        string `mapstructure:"file_pattern"`
	ConnectionString   string `mapstructure:"connection_string"`
}

// IsEmpty returns true if the source config has no meaningful configuration.
func (c *SourceConfig) IsEmpty() bool {
	return c.StorageAccountName == "" && c.ConnectionString == "" && c.ContainerName == ""
}

// AggregatedSource represents sources aggregated by storage account for efficiency.
// This allows sharing a single Azure client connection for multiple containers.
type AggregatedSource struct {
	StorageAccountName string
	ConnectionString   string
	Containers         []ContainerSource
}

// ContainerSource represents a single container with its file pattern.
type ContainerSource struct {
	ContainerName string
	FilePattern   string
}

// GetAggregatedSources returns the list of sources aggregated by storage account.
// This is computed once during validation and cached.
func (c *Config) GetAggregatedSources() []AggregatedSource {
	return c.aggregatedSources
}

// OutputConfig holds output file configuration.
type OutputConfig struct {
	Directory  string `mapstructure:"directory"`
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"` // MB
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"` // days
	Gzip       bool   `mapstructure:"gzip"`    // Enable gzip compression
}

// MetricsConfig holds Prometheus metrics configuration.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Address string `mapstructure:"address"`
}

// ProcessingConfig holds processing behavior configuration.
type ProcessingConfig struct {
	Workers            int           `mapstructure:"workers"`
	DryRun             bool          `mapstructure:"dry_run"`
	DeleteAfterProcess bool          `mapstructure:"delete_after_process"`
	BackoffEnabled     bool          `mapstructure:"backoff_enabled"`
	MinFreeSpace       string        `mapstructure:"min_free_space"` // Minimum free space with unit (e.g., "1GB", "500MB", "100MiB")
	RetryAttempts      int           `mapstructure:"retry_attempts"`
	RetryDelay         time.Duration `mapstructure:"retry_delay"`
	TempDir            string        `mapstructure:"temp_dir"`
	SortOrder          string        `mapstructure:"sort_order"`    // "newest" or "oldest" (default: "oldest")
	BatchLimit         int           `mapstructure:"batch_limit"`   // Max blobs per batch (0 = unlimited)
	OnceLimit          int           `mapstructure:"once_limit"`    // Max blobs for --once mode (default: 10)
	MinAge             time.Duration `mapstructure:"min_age"`       // Minimum age of blobs to process (0 = no minimum)
	MaxAge             time.Duration `mapstructure:"max_age"`       // Maximum age of blobs to process (0 = no maximum)
	PollInterval       time.Duration `mapstructure:"poll_interval"` // Interval between polling for new blobs (default: 1m)
}

// ParseByteSize parses a byte size string with units (e.g., "1GB", "500MB", "100MiB").
// Supported units: B, KB, KiB, MB, MiB, GB, GiB, TB, TiB
// If no unit is specified, bytes are assumed.
func ParseByteSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}

	// Regex to match number and optional unit
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
	var multiplier float64

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
	default:
		return 0, fmt.Errorf("unknown byte size unit: %s", unit)
	}

	return uint64(value * multiplier), nil
}

// GetMinFreeSpaceBytes returns the minimum free space in bytes.
func (c *ProcessingConfig) GetMinFreeSpaceBytes() uint64 {
	bytes, err := ParseByteSize(c.MinFreeSpace)
	if err != nil {
		// Default to 256MiB if parsing fails
		return 256 * 1024 * 1024
	}
	return bytes
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // text, json
}

// Load loads configuration from file, environment, and flags.
func Load(configPath string) (*Config, error) {
	// Use the global viper instance to respect CLI flag bindings
	v := viper.GetViper()

	// Set defaults
	setDefaults(v)

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.adda")
		v.AddConfigPath("/etc/adda")
	}

	// Environment variables
	v.SetEnvPrefix("ADDA")
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values.
func setDefaults(v *viper.Viper) {
	// Output defaults - rotation disabled by default (0 = disabled)
	v.SetDefault("output.directory", ".")
	v.SetDefault("output.filename", "output.ndjson")
	v.SetDefault("output.max_size", 0)    // 0 = no size-based rotation (disabled by default)
	v.SetDefault("output.max_backups", 0) // 0 = keep all old files
	v.SetDefault("output.max_age", 0)     // 0 = don't remove old files based on age
	v.SetDefault("output.gzip", false)

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.address", ":9090")

	// Processing defaults
	v.SetDefault("processing.workers", 1)
	v.SetDefault("processing.dry_run", false)
	v.SetDefault("processing.delete_after_process", true)
	v.SetDefault("processing.backoff_enabled", true)
	v.SetDefault("processing.min_free_space", "256MiB") // Default 256MiB minimum free space
	v.SetDefault("processing.retry_attempts", 3)
	v.SetDefault("processing.retry_delay", "5s")
	v.SetDefault("processing.temp_dir", os.TempDir())
	v.SetDefault("processing.sort_order", "oldest") // Process oldest blobs first
	v.SetDefault("processing.batch_limit", 0)       // 0 = unlimited
	v.SetDefault("processing.once_limit", 10)       // Default limit for --once mode
	v.SetDefault("processing.min_age", "0s")        // No minimum age
	v.SetDefault("processing.max_age", "0s")        // No maximum age
	v.SetDefault("processing.poll_interval", "1m")  // Poll every minute

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Collect and validate sources
	if err := c.collectAndAggregateSources(); err != nil {
		return err
	}

	// Validate output
	if c.Output.Directory == "" {
		return fmt.Errorf("output.directory is required")
	}
	if c.Output.Filename == "" {
		return fmt.Errorf("output.filename is required")
	}
	if c.Output.MaxSize < 0 {
		return fmt.Errorf("output.max_size must be non-negative")
	}
	if c.Output.MaxBackups < 0 {
		return fmt.Errorf("output.max_backups must be non-negative")
	}
	if c.Output.MaxAge < 0 {
		return fmt.Errorf("output.max_age must be non-negative")
	}

	// Validate processing
	if c.Processing.Workers < 1 {
		return fmt.Errorf("processing.workers must be at least 1")
	}
	if c.Processing.MinFreeSpace != "" {
		if _, err := ParseByteSize(c.Processing.MinFreeSpace); err != nil {
			return fmt.Errorf("processing.min_free_space: %w", err)
		}
	}
	if c.Processing.RetryAttempts < 0 {
		return fmt.Errorf("processing.retry_attempts must be non-negative")
	}
	if c.Processing.BatchLimit < 0 {
		return fmt.Errorf("processing.batch_limit must be non-negative")
	}
	if c.Processing.OnceLimit < 0 {
		return fmt.Errorf("processing.once_limit must be non-negative")
	}
	validSortOrders := map[string]bool{"newest": true, "oldest": true, "": true}
	if !validSortOrders[c.Processing.SortOrder] {
		return fmt.Errorf("processing.sort_order must be 'newest' or 'oldest'")
	}
	if c.Processing.MinAge < 0 {
		return fmt.Errorf("processing.min_age must be non-negative")
	}
	if c.Processing.MaxAge < 0 {
		return fmt.Errorf("processing.max_age must be non-negative")
	}
	if c.Processing.MinAge > 0 && c.Processing.MaxAge > 0 && c.Processing.MinAge > c.Processing.MaxAge {
		return fmt.Errorf("processing.min_age cannot be greater than processing.max_age")
	}
	if c.Processing.PollInterval < 0 {
		return fmt.Errorf("processing.poll_interval must be non-negative")
	}
	if c.Processing.TempDir != "" {
		if info, err := os.Stat(c.Processing.TempDir); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to check temp_dir: %w", err)
			}
			// Directory doesn't exist, try to create it
			if err := os.MkdirAll(c.Processing.TempDir, 0755); err != nil {
				return fmt.Errorf("failed to create temp_dir %s: %w", c.Processing.TempDir, err)
			}
		} else if !info.IsDir() {
			return fmt.Errorf("processing.temp_dir %s is not a directory", c.Processing.TempDir)
		}
	}

	// Validate logging
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if c.Logging.Level != "" && !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("logging.level must be one of: debug, info, warn, error")
	}
	validLogFormats := map[string]bool{"text": true, "json": true}
	if c.Logging.Format != "" && !validLogFormats[c.Logging.Format] {
		return fmt.Errorf("logging.format must be one of: text, json")
	}

	// Validate parsers
	if len(c.Parsers) == 0 {
		// Default to NDJSON parser (most common format for log data)
		c.Parsers = []parser.Config{
			{ID: "ndjson", Type: "ndjson"},
		}
	}

	parserIDs := make(map[string]bool)
	for i, p := range c.Parsers {
		if p.ID == "" {
			return fmt.Errorf("parsers[%d].id is required", i)
		}
		if parserIDs[p.ID] {
			return fmt.Errorf("parsers[%d].id '%s' is duplicate", i, p.ID)
		}
		parserIDs[p.ID] = true

		if p.Type == "" {
			return fmt.Errorf("parsers[%d].type is required", i)
		}
		if p.Type != "ndjson" && p.Type != "json" && p.Type != "external" {
			return fmt.Errorf("parsers[%d].type must be 'ndjson', 'json', or 'external', got '%s'", i, p.Type)
		}
		if p.Type == "external" && p.Command == "" {
			return fmt.Errorf("parsers[%d].command is required for external parsers", i)
		}
	}

	return nil
}

// GetWriterConfig returns the writer configuration.
func (c *Config) GetWriterConfig() writer.Config {
	filename := filepath.Join(c.Output.Directory, c.Output.Filename)
	return writer.Config{
		Filename:   filename,
		MaxSize:    c.Output.MaxSize,
		MaxBackups: c.Output.MaxBackups,
		MaxAge:     c.Output.MaxAge,
		Gzip:       c.Output.Gzip,
	}
}

// EnsureOutputDir creates the output directory if it doesn't exist.
func (c *Config) EnsureOutputDir() error {
	if err := os.MkdirAll(c.Output.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", c.Output.Directory, err)
	}
	return nil
}

// collectAndAggregateSources collects sources from both 'source' and 'sources' config,
// validates them, and aggregates by storage account for API efficiency.
func (c *Config) collectAndAggregateSources() error {
	// Collect all source configs
	var allSources []SourceConfig

	// Add single source if configured
	if !c.Source.IsEmpty() {
		if err := validateSourceConfig(&c.Source, "source"); err != nil {
			return err
		}
		allSources = append(allSources, c.Source)
	}

	// Add sources from the list
	for i, src := range c.Sources {
		if src.IsEmpty() {
			continue
		}
		if err := validateSourceConfig(&src, fmt.Sprintf("sources[%d]", i)); err != nil {
			return err
		}
		allSources = append(allSources, src)
	}

	// Ensure at least one source is configured
	if len(allSources) == 0 {
		return fmt.Errorf("at least one source must be configured via 'source' or 'sources'")
	}

	// Aggregate sources by storage account (or connection string) for efficiency
	c.aggregatedSources = aggregateSourcesByAccount(allSources)

	return nil
}

// validateSourceConfig validates a single source configuration.
func validateSourceConfig(src *SourceConfig, prefix string) error {
	if src.StorageAccountName == "" && src.ConnectionString == "" {
		return fmt.Errorf("%s: storage_account_name or connection_string is required", prefix)
	}
	if src.ContainerName == "" {
		return fmt.Errorf("%s: container_name is required", prefix)
	}
	return nil
}

// aggregateSourcesByAccount groups sources by their storage account (or connection string)
// to allow sharing Azure client connections for efficiency.
func aggregateSourcesByAccount(sources []SourceConfig) []AggregatedSource {
	// Use a map to group by account identifier
	// Key is either connection_string or storage_account_name
	type accountKey struct {
		connectionString   string
		storageAccountName string
	}

	aggregated := make(map[accountKey]*AggregatedSource)
	var order []accountKey // Preserve insertion order

	for _, src := range sources {
		key := accountKey{
			connectionString:   src.ConnectionString,
			storageAccountName: src.StorageAccountName,
		}

		if existing, ok := aggregated[key]; ok {
			// Check for duplicate container+pattern combinations
			isDuplicate := false
			for _, cs := range existing.Containers {
				if cs.ContainerName == src.ContainerName && cs.FilePattern == src.FilePattern {
					isDuplicate = true
					break
				}
			}
			if !isDuplicate {
				existing.Containers = append(existing.Containers, ContainerSource{
					ContainerName: src.ContainerName,
					FilePattern:   src.FilePattern,
				})
			}
		} else {
			aggregated[key] = &AggregatedSource{
				StorageAccountName: src.StorageAccountName,
				ConnectionString:   src.ConnectionString,
				Containers: []ContainerSource{
					{
						ContainerName: src.ContainerName,
						FilePattern:   src.FilePattern,
					},
				},
			}
			order = append(order, key)
		}
	}

	// Convert map to slice preserving order
	result := make([]AggregatedSource, 0, len(order))
	for _, key := range order {
		result = append(result, *aggregated[key])
	}

	return result
}
