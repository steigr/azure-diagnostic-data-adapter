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

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint64
		wantErr bool
	}{
		// Bytes
		{"empty string", "", 0, false},
		{"zero", "0", 0, false},
		{"bytes implicit", "1024", 1024, false},
		{"bytes explicit", "1024B", 1024, false},
		{"bytes lowercase", "1024b", 1024, false},

		// Kilobytes (decimal)
		{"KB", "1KB", 1000, false},
		{"KB lowercase", "1kb", 1000, false},
		{"KB with decimal", "1.5KB", 1500, false},

		// Kibibytes (binary)
		{"KiB", "1KiB", 1024, false},
		{"KiB lowercase", "1kib", 1024, false},

		// Megabytes (decimal)
		{"MB", "1MB", 1000000, false},
		{"MB lowercase", "1mb", 1000000, false},
		{"MB with value", "500MB", 500000000, false},

		// Mebibytes (binary)
		{"MiB", "1MiB", 1048576, false},
		{"MiB lowercase", "1mib", 1048576, false},
		{"MiB with value", "100MiB", 104857600, false},

		// Gigabytes (decimal)
		{"GB", "1GB", 1000000000, false},
		{"GB lowercase", "1gb", 1000000000, false},

		// Gibibytes (binary)
		{"GiB", "1GiB", 1073741824, false},
		{"GiB lowercase", "1gib", 1073741824, false},

		// Terabytes (decimal)
		{"TB", "1TB", 1000000000000, false},

		// Tebibytes (binary)
		{"TiB", "1TiB", 1099511627776, false},

		// With spaces
		{"with leading space", " 1GB", 1000000000, false},
		{"with trailing space", "1GB ", 1000000000, false},

		// Errors
		{"invalid format", "abc", 0, true},
		{"invalid unit", "1XB", 0, true},
		{"negative not supported", "-1GB", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseByteSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseByteSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseByteSize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProcessingConfig_GetMinFreeSpaceBytes(t *testing.T) {
	tests := []struct {
		name         string
		minFreeSpace string
		want         uint64
	}{
		{"1GB", "1GB", 1000000000},
		{"1GiB", "1GiB", 1073741824},
		{"500MB", "500MB", 500000000},
		{"100MiB", "100MiB", 104857600},
		{"empty returns 0", "", 0},                           // Empty means no minimum
		{"invalid defaults to 256MiB", "invalid", 268435456}, // Invalid returns default 256MiB
		{"256MiB", "256MiB", 268435456},                      // Default value
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ProcessingConfig{
				MinFreeSpace: tt.minFreeSpace,
			}
			if got := cfg.GetMinFreeSpaceBytes(); got != tt.want {
				t.Errorf("GetMinFreeSpaceBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_Validate_MinFreeSpace(t *testing.T) {
	tests := []struct {
		name         string
		minFreeSpace string
		wantErr      bool
	}{
		{"valid GB", "1GB", false},
		{"valid MB", "500MB", false},
		{"valid MiB", "100MiB", false},
		{"valid bytes", "1048576", false},
		{"empty is valid", "", false},
		{"invalid format", "invalid", true},
		{"invalid unit", "1XB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      "container",
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers:      1,
					MinFreeSpace: tt.minFreeSpace,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() with MinFreeSpace=%q error = %v, wantErr %v", tt.minFreeSpace, err, tt.wantErr)
			}
		})
	}
}

func TestSourceConfig_GetContainerNames(t *testing.T) {
	tests := []struct {
		name           string
		containerName  string
		containerNames []string
		want           []string
	}{
		{
			name:           "single container_name",
			containerName:  "container1",
			containerNames: nil,
			want:           []string{"container1"},
		},
		{
			name:           "multiple container_names",
			containerName:  "",
			containerNames: []string{"container1", "container2", "container3"},
			want:           []string{"container1", "container2", "container3"},
		},
		{
			name:           "both singular and plural",
			containerName:  "primary",
			containerNames: []string{"secondary", "tertiary"},
			want:           []string{"primary", "secondary", "tertiary"},
		},
		{
			name:           "duplicates are removed",
			containerName:  "container1",
			containerNames: []string{"container1", "container2"},
			want:           []string{"container1", "container2"},
		},
		{
			name:           "empty strings are ignored",
			containerName:  "container1",
			containerNames: []string{"", "container2", ""},
			want:           []string{"container1", "container2"},
		},
		{
			name:           "all duplicates",
			containerName:  "same",
			containerNames: []string{"same", "same", "same"},
			want:           []string{"same"},
		},
		{
			name:           "no containers",
			containerName:  "",
			containerNames: nil,
			want:           []string{},
		},
		{
			name:           "only empty strings in plural",
			containerName:  "",
			containerNames: []string{"", "", ""},
			want:           []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SourceConfig{
				ContainerName:  tt.containerName,
				ContainerNames: tt.containerNames,
			}
			got := cfg.GetContainerNames()
			if len(got) != len(tt.want) {
				t.Errorf("GetContainerNames() returned %d items, want %d", len(got), len(tt.want))
				return
			}
			for i, name := range got {
				if name != tt.want[i] {
					t.Errorf("GetContainerNames()[%d] = %v, want %v", i, name, tt.want[i])
				}
			}
		})
	}
}

func TestConfig_Validate_MultipleContainers(t *testing.T) {
	tests := []struct {
		name           string
		containerName  string
		containerNames []string
		wantErr        bool
	}{
		{
			name:          "valid with singular",
			containerName: "container1",
			wantErr:       false,
		},
		{
			name:           "valid with plural",
			containerNames: []string{"container1", "container2"},
			wantErr:        false,
		},
		{
			name:           "valid with both",
			containerName:  "primary",
			containerNames: []string{"secondary"},
			wantErr:        false,
		},
		{
			name:    "invalid with neither",
			wantErr: true,
		},
		{
			name:           "invalid with only empty strings",
			containerNames: []string{"", ""},
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Source: SourceConfig{
					StorageAccountName: "test",
					ContainerName:      tt.containerName,
					ContainerNames:     tt.containerNames,
				},
				Output: OutputConfig{
					Directory: ".",
					Filename:  "output.ndjson",
				},
				Processing: ProcessingConfig{
					Workers: 1,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
