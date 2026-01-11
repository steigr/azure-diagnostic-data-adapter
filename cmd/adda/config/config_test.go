package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
				},
			},
			wantErr: false,
		},
		{
			name: "missing storage account and connection string",
			cfg: Config{
				Source: SourceConfig{
					ContainerName: "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "missing container name",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "missing output directory",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Filename: "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "valid with connection string",
			cfg: Config{
				Source: SourceConfig{
					ConnectionString: "DefaultEndpointsProtocol=https;AccountName=test",
					ContainerName:    "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: false,
		},
		{
			name: "invalid parser type",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "invalid", Type: "unknown"},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "external parser without command",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "ext", Type: "external"},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "invalid workers count",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 0,
				},
			},
			wantErr: true,
		},
		{
			name: "negative max_size",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
					MaxSize:   -1,
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "invalid log level",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
				Logging: LoggingConfig{
					Level: "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid log format",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{Workers: 1},
				Logging: LoggingConfig{
					Format: "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate parser IDs",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "ndjson", Type: "ndjson"},
					{ID: "ndjson", Type: "ndjson"},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: true,
		},
		{
			name: "valid ndjson parser",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "ndjson", Type: "ndjson", FilePattern: `.*\.json$`},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: false,
		},
		{
			name: "valid json parser",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "json", Type: "json", FilePattern: `.*\.json-array$`},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: false,
		},
		{
			name: "valid mixed parsers",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Parsers: []parser.Config{
					{ID: "ndjson", Type: "ndjson", FilePattern: `.*\.json$`},
					{ID: "json", Type: "json", FilePattern: `.*\.json-array$`},
					{ID: "csv", Type: "external", FilePattern: `.*\.csv$`, Command: "/usr/bin/csv2json"},
				},
				Processing: ProcessingConfig{Workers: 1},
			},
			wantErr: false,
		},
		{
			name: "valid min_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MinAge:  15 * time.Second,
				},
			},
			wantErr: false,
		},
		{
			name: "valid max_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MaxAge:  24 * time.Hour,
				},
			},
			wantErr: false,
		},
		{
			name: "valid min_age and max_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MinAge:  15 * time.Second,
					MaxAge:  24 * time.Hour,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid min_age greater than max_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MinAge:  24 * time.Hour,
					MaxAge:  1 * time.Hour,
				},
			},
			wantErr: true,
		},
		{
			name: "negative min_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MinAge:  -1 * time.Second,
				},
			},
			wantErr: true,
		},
		{
			name: "negative max_age",
			cfg: Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
					MaxAge:  -1 * time.Second,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_GetWriterConfig(t *testing.T) {
	cfg := Config{
		Output: OutputConfig{
			Directory:  "/var/log",
			Filename:   "test.ndjson",
			MaxSize:    50,
			MaxBackups: 5,
			MaxAge:     7,
		},
	}

	wc := cfg.GetWriterConfig()

	expectedPath := filepath.Join("/var/log", "test.ndjson")
	if wc.Filename != expectedPath {
		t.Errorf("Filename = %v, want %v", wc.Filename, expectedPath)
	}
	if wc.MaxSize != 50 {
		t.Errorf("MaxSize = %v, want 50", wc.MaxSize)
	}
	if wc.MaxBackups != 5 {
		t.Errorf("MaxBackups = %v, want 5", wc.MaxBackups)
	}
	if wc.MaxAge != 7 {
		t.Errorf("MaxAge = %v, want 7", wc.MaxAge)
	}
}

func TestConfig_EnsureOutputDir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested", "output")

	cfg := Config{
		Output: OutputConfig{
			Directory: subdir,
		},
	}

	if err := cfg.EnsureOutputDir(); err != nil {
		t.Fatalf("EnsureOutputDir() error = %v", err)
	}

	if _, err := os.Stat(subdir); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("Load() expected error for missing file")
	}
}
