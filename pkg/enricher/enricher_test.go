package enricher

import (
	"testing"
	"time"
)

func TestEnricher_New(t *testing.T) {
	tests := []struct {
		name     string
		template string
		wantErr  bool
	}{
		{"empty template", "", false},
		{"valid template", `{"source": "{{ .Metadata.BlobName }}"}`, false},
		{"invalid template", `{{`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnricher_Enrich_NoTemplate(t *testing.T) {
	e, err := New("")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record := map[string]any{"key": "value"}
	metadata := Metadata{BlobName: "test.json"}

	result, err := e.Enrich(record, metadata)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("result[key] = %v, want value", result["key"])
	}
}

func TestEnricher_Enrich_WithTemplate(t *testing.T) {
	e, err := New(`{"source": "{{ .Metadata.BlobName }}", "container": "{{ .Metadata.ContainerName }}"}`)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record := map[string]any{"key": "value"}
	metadata := Metadata{
		BlobName:      "test.json",
		ContainerName: "my-container",
	}

	result, err := e.Enrich(record, metadata)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if result["key"] != "value" {
		t.Errorf("result[key] = %v, want value", result["key"])
	}
	if result["source"] != "test.json" {
		t.Errorf("result[source] = %v, want test.json", result["source"])
	}
	if result["container"] != "my-container" {
		t.Errorf("result[container] = %v, want my-container", result["container"])
	}
}

func TestEnricher_Enrich_OverridesRecord(t *testing.T) {
	e, err := New(`{"key": "enriched"}`)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record := map[string]any{"key": "original"}
	metadata := Metadata{}

	result, err := e.Enrich(record, metadata)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if result["key"] != "enriched" {
		t.Errorf("result[key] = %v, want enriched", result["key"])
	}
}

func TestEnricher_EnrichBatch(t *testing.T) {
	e, err := New(`{"source": "{{ .Metadata.BlobName }}"}`)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records := []map[string]any{
		{"id": 1},
		{"id": 2},
		{"id": 3},
	}
	metadata := Metadata{
		BlobName:    "batch.json",
		ProcessedAt: time.Now(),
	}

	results, err := e.EnrichBatch(records, metadata)
	if err != nil {
		t.Fatalf("EnrichBatch() error = %v", err)
	}

	if len(results) != 3 {
		t.Errorf("EnrichBatch() returned %d results, want 3", len(results))
	}

	for i, result := range results {
		if result["source"] != "batch.json" {
			t.Errorf("results[%d][source] = %v, want batch.json", i, result["source"])
		}
	}
}

func TestNewMetadata_PathParsing(t *testing.T) {
	tests := []struct {
		name          string
		blobName      string
		wantDirectory string
		wantFileName  string
		wantExtension string
		wantPathParts []string
	}{
		{
			name:          "simple file",
			blobName:      "data.json",
			wantDirectory: "",
			wantFileName:  "data.json",
			wantExtension: "json",
			wantPathParts: []string{"data.json"},
		},
		{
			name:          "nested path",
			blobName:      "logs/app/2026/01/data.json",
			wantDirectory: "logs/app/2026/01",
			wantFileName:  "data.json",
			wantExtension: "json",
			wantPathParts: []string{"logs", "app", "2026", "01", "data.json"},
		},
		{
			name:          "single folder",
			blobName:      "logs/data.json",
			wantDirectory: "logs",
			wantFileName:  "data.json",
			wantExtension: "json",
			wantPathParts: []string{"logs", "data.json"},
		},
		{
			name:          "no extension",
			blobName:      "logs/datafile",
			wantDirectory: "logs",
			wantFileName:  "datafile",
			wantExtension: "",
			wantPathParts: []string{"logs", "datafile"},
		},
		{
			name:          "multiple dots",
			blobName:      "logs/data.backup.json",
			wantDirectory: "logs",
			wantFileName:  "data.backup.json",
			wantExtension: "json",
			wantPathParts: []string{"logs", "data.backup.json"},
		},
		{
			name:          "empty blob name",
			blobName:      "",
			wantDirectory: "",
			wantFileName:  ".",
			wantExtension: "",
			wantPathParts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMetadata(tt.blobName, "container", "account", time.Now(), 100, "application/json", time.Now())

			if m.Directory != tt.wantDirectory {
				t.Errorf("Directory = %v, want %v", m.Directory, tt.wantDirectory)
			}
			if m.FileName != tt.wantFileName {
				t.Errorf("FileName = %v, want %v", m.FileName, tt.wantFileName)
			}
			if m.Extension != tt.wantExtension {
				t.Errorf("Extension = %v, want %v", m.Extension, tt.wantExtension)
			}
			if len(m.PathParts) != len(tt.wantPathParts) {
				t.Errorf("PathParts length = %v, want %v", len(m.PathParts), len(tt.wantPathParts))
			} else {
				for i, part := range m.PathParts {
					if part != tt.wantPathParts[i] {
						t.Errorf("PathParts[%d] = %v, want %v", i, part, tt.wantPathParts[i])
					}
				}
			}
		})
	}
}

func TestEnricher_Enrich_HierarchicalPath(t *testing.T) {
	e, err := New(`{
		"directory": "{{ .Metadata.Directory }}",
		"filename": "{{ .Metadata.FileName }}",
		"extension": "{{ .Metadata.Extension }}",
		"first_folder": "{{ index .Metadata.PathParts 0 }}",
		"second_folder": "{{ index .Metadata.PathParts 1 }}"
	}`)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record := map[string]any{"key": "value"}
	metadata := NewMetadata("logs/app/data.json", "container", "account", time.Now(), 100, "application/json", time.Now())

	result, err := e.Enrich(record, metadata)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if result["directory"] != "logs/app" {
		t.Errorf("result[directory] = %v, want logs/app", result["directory"])
	}
	if result["filename"] != "data.json" {
		t.Errorf("result[filename] = %v, want data.json", result["filename"])
	}
	if result["extension"] != "json" {
		t.Errorf("result[extension] = %v, want json", result["extension"])
	}
	if result["first_folder"] != "logs" {
		t.Errorf("result[first_folder] = %v, want logs", result["first_folder"])
	}
	if result["second_folder"] != "app" {
		t.Errorf("result[second_folder] = %v, want app", result["second_folder"])
	}
}

func TestEnricher_PathFunctions(t *testing.T) {
	tmpl := `{
		"dir": "{{ pathDir .Metadata.BlobName }}",
		"base": "{{ pathBase .Metadata.BlobName }}",
		"ext": "{{ pathExt .Metadata.BlobName }}",
		"part0": "{{ pathPart .Metadata.PathParts 0 }}",
		"part1": "{{ pathPart .Metadata.PathParts 1 }}",
		"joined": "{{ pathJoin .Metadata.Directory "newfile.json" }}"
	}`
	e, err := New(tmpl)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	record := map[string]any{}
	metadata := NewMetadata("logs/2026/data.json", "container", "account", time.Now(), 100, "application/json", time.Now())

	result, err := e.Enrich(record, metadata)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	if result["dir"] != "logs/2026" {
		t.Errorf("result[dir] = %v, want logs/2026", result["dir"])
	}
	if result["base"] != "data.json" {
		t.Errorf("result[base] = %v, want data.json", result["base"])
	}
	if result["ext"] != "json" {
		t.Errorf("result[ext] = %v, want json", result["ext"])
	}
	if result["part0"] != "logs" {
		t.Errorf("result[part0] = %v, want logs", result["part0"])
	}
	if result["part1"] != "2026" {
		t.Errorf("result[part1] = %v, want 2026", result["part1"])
	}
	if result["joined"] != "logs/2026/newfile.json" {
		t.Errorf("result[joined] = %v, want logs/2026/newfile.json", result["joined"])
	}
}
