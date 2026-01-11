// Package config provides configuration handling for the Azure Diagnostic Data Adapter.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/writer"
)

// Config holds the complete application configuration.
type Config struct {
	// Source configuration
	Source SourceConfig `mapstructure:"source"`

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
}

// SourceConfig holds Azure Storage Account configuration.
type SourceConfig struct {
	StorageAccountName string `mapstructure:"storage_account_name"`
	ContainerName      string `mapstructure:"container_name"`
	FilePattern        string `mapstructure:"file_pattern"`
	ConnectionString   string `mapstructure:"connection_string"`
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
	MinFreeSpaceGB     int           `mapstructure:"min_free_space_gb"`
	RetryAttempts      int           `mapstructure:"retry_attempts"`
	RetryDelay         time.Duration `mapstructure:"retry_delay"`
	TempDir            string        `mapstructure:"temp_dir"`
	SortOrder          string        `mapstructure:"sort_order"`  // "newest" or "oldest" (default: "oldest")
	BatchLimit         int           `mapstructure:"batch_limit"` // Max blobs per batch (0 = unlimited)
	OnceLimit          int           `mapstructure:"once_limit"`  // Max blobs for --once mode (default: 10)
	MinAge             time.Duration `mapstructure:"min_age"`     // Minimum age of blobs to process (0 = no minimum)
	MaxAge             time.Duration `mapstructure:"max_age"`     // Maximum age of blobs to process (0 = no maximum)
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`  // debug, info, warn, error
	Format string `mapstructure:"format"` // text, json
}

// Load loads configuration from file, environment, and flags.
func Load(configPath string) (*Config, error) {
	v := viper.New()

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
	// Output defaults
	v.SetDefault("output.directory", ".")
	v.SetDefault("output.filename", "output.ndjson")
	v.SetDefault("output.max_size", 100) // 100 MB
	v.SetDefault("output.max_backups", 3)
	v.SetDefault("output.max_age", 28) // 28 days
	v.SetDefault("output.gzip", false)

	// Metrics defaults
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.address", ":9090")

	// Processing defaults
	v.SetDefault("processing.workers", 1)
	v.SetDefault("processing.dry_run", false)
	v.SetDefault("processing.delete_after_process", true)
	v.SetDefault("processing.backoff_enabled", true)
	v.SetDefault("processing.min_free_space_gb", 1)
	v.SetDefault("processing.retry_attempts", 3)
	v.SetDefault("processing.retry_delay", "5s")
	v.SetDefault("processing.temp_dir", os.TempDir())
	v.SetDefault("processing.sort_order", "oldest") // Process oldest blobs first
	v.SetDefault("processing.batch_limit", 0)       // 0 = unlimited
	v.SetDefault("processing.once_limit", 10)       // Default limit for --once mode
	v.SetDefault("processing.min_age", "0s")        // No minimum age
	v.SetDefault("processing.max_age", "0s")        // No maximum age

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate source
	if c.Source.StorageAccountName == "" && c.Source.ConnectionString == "" {
		return fmt.Errorf("source.storage_account_name or source.connection_string is required")
	}
	if c.Source.ContainerName == "" {
		return fmt.Errorf("source.container_name is required")
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
	if c.Processing.MinFreeSpaceGB < 0 {
		return fmt.Errorf("processing.min_free_space_gb must be non-negative")
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
