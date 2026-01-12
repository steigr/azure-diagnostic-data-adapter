// Package enricher provides metadata enrichment for parsed records.
package enricher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"text/template"
	"time"
)

// Enricher adds metadata to parsed records using Go templates.
type Enricher struct {
	tmpl *template.Template
}

// Metadata holds metadata about the blob being processed.
type Metadata struct {
	BlobName       string
	ContainerName  string
	StorageAccount string
	ProcessedAt    time.Time
	Size           int64
	ContentType    string
	LastModified   time.Time

	// Hierarchical path components extracted from BlobName
	Directory string   // Parent directory path (e.g., "logs/app/2026")
	FileName  string   // Base filename (e.g., "data.json")
	Extension string   // File extension without dot (e.g., "json")
	PathParts []string // All path components (e.g., ["logs", "app", "2026", "data.json"])

	// FileMatch holds named capture groups from the parser's file_pattern regex
	FileMatch map[string]string
}

// NewMetadata creates a Metadata instance with hierarchical path components parsed from blob name.
func NewMetadata(blobName, containerName, storageAccount string, processedAt time.Time, size int64, contentType string, lastModified time.Time) Metadata {
	return NewMetadataWithFileMatch(blobName, containerName, storageAccount, processedAt, size, contentType, lastModified, nil)
}

// NewMetadataWithFileMatch creates a Metadata instance with hierarchical path components and file match data.
func NewMetadataWithFileMatch(blobName, containerName, storageAccount string, processedAt time.Time, size int64, contentType string, lastModified time.Time, fileMatch map[string]string) Metadata {
	m := Metadata{
		BlobName:       blobName,
		ContainerName:  containerName,
		StorageAccount: storageAccount,
		ProcessedAt:    processedAt,
		Size:           size,
		ContentType:    contentType,
		LastModified:   lastModified,
		FileMatch:      fileMatch,
	}

	// Initialize empty map if nil
	if m.FileMatch == nil {
		m.FileMatch = make(map[string]string)
	}

	// Parse hierarchical path components
	m.Directory = path.Dir(blobName)
	if m.Directory == "." {
		m.Directory = ""
	}

	m.FileName = path.Base(blobName)
	m.Extension = strings.TrimPrefix(path.Ext(blobName), ".")

	// Split into path parts
	if blobName != "" {
		m.PathParts = strings.Split(blobName, "/")
	}

	return m
}

// Context provides the template context for enrichment.
type Context struct {
	Record   map[string]any
	Metadata Metadata
}

// New creates a new Enricher with the given template string.
// If templateStr is empty, no enrichment is performed.
func New(templateStr string) (*Enricher, error) {
	if templateStr == "" {
		return &Enricher{}, nil
	}

	funcs := template.FuncMap{
		"now": time.Now,
		"json": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"formatTime": func(t time.Time, layout string) string {
			return t.Format(layout)
		},
		// Path manipulation functions
		"pathDir": func(p string) string {
			dir := path.Dir(p)
			if dir == "." {
				return ""
			}
			return dir
		},
		"pathBase": path.Base,
		"pathExt": func(p string) string {
			return strings.TrimPrefix(path.Ext(p), ".")
		},
		"pathSplit": func(p string) []string {
			if p == "" {
				return nil
			}
			return strings.Split(p, "/")
		},
		"pathPart": func(parts []string, index int) string {
			if index < 0 || index >= len(parts) {
				return ""
			}
			return parts[index]
		},
		"pathJoin": path.Join,
	}

	tmpl, err := template.New("enricher").Funcs(funcs).Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse enrichment template: %w", err)
	}

	return &Enricher{tmpl: tmpl}, nil
}

// Enrich adds metadata to a record using the configured template.
// The template should produce a valid JSON object that will be merged with the record.
func (e *Enricher) Enrich(record map[string]any, metadata Metadata) (map[string]any, error) {
	if e.tmpl == nil {
		// No enrichment template configured, return record as-is
		return record, nil
	}

	ctx := Context{
		Record:   record,
		Metadata: metadata,
	}

	var buf bytes.Buffer
	if err := e.tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("failed to execute enrichment template: %w", err)
	}

	// Parse the template output as JSON
	var enrichment map[string]any
	if err := json.Unmarshal(buf.Bytes(), &enrichment); err != nil {
		return nil, fmt.Errorf("enrichment template did not produce valid JSON: %w, output: %s", err, buf.String())
	}

	// Merge enrichment into record (enrichment values override record values)
	result := make(map[string]any)
	for k, v := range record {
		result[k] = v
	}
	for k, v := range enrichment {
		result[k] = v
	}

	return result, nil
}

// EnrichBatch enriches multiple records with the same metadata.
func (e *Enricher) EnrichBatch(records []map[string]any, metadata Metadata) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(records))
	for _, record := range records {
		enriched, err := e.Enrich(record, metadata)
		if err != nil {
			return nil, err
		}
		result = append(result, enriched)
	}
	return result, nil
}
