# Enrichment Templates

The Azure Diagnostic Data Adapter uses Go templates to enrich parsed records with metadata. This document provides a comprehensive guide to using enrichment templates.

## Overview

Enrichment templates produce valid JSON that is merged with each parsed record. The template has access to the original record and metadata about the source blob.

## Available Variables

### Record Data

| Variable | Type | Description |
|----------|------|-------------|
| `.Record` | `map[string]any` | The original parsed record |

### Metadata Fields

| Variable | Type | Description |
|----------|------|-------------|
| `.Metadata.BlobName` | `string` | Full path of the source blob |
| `.Metadata.ContainerName` | `string` | Azure container name |
| `.Metadata.StorageAccount` | `string` | Azure storage account name |
| `.Metadata.ProcessedAt` | `time.Time` | When the record was processed |
| `.Metadata.LastModified` | `time.Time` | When the blob was last modified |
| `.Metadata.Size` | `int64` | Blob size in bytes |
| `.Metadata.ContentType` | `string` | Blob content type |

### Hierarchical Path Fields

These fields are automatically parsed from the blob name:

| Variable | Type | Description | Example |
|----------|------|-------------|---------|
| `.Metadata.Directory` | `string` | Parent directory path | `logs/app/2026` |
| `.Metadata.FileName` | `string` | Base filename | `data.json` |
| `.Metadata.Extension` | `string` | File extension (no dot) | `json` |
| `.Metadata.PathParts` | `[]string` | All path components | `["logs", "app", "2026", "data.json"]` |

### FileMatch Fields

Named capture groups from the parser's `file_pattern` regex are available via `.Metadata.FileMatch`:

| Variable | Type | Description |
|----------|------|-------------|
| `.Metadata.FileMatch` | `map[string]string` | Map of capture group names to their matched values |

**Example:** With parser file_pattern `logs/(?P<year>\d{4})/(?P<month>\d{2})/.*\.json$` and blob name `logs/2026/01/data.json`:

| Expression | Value |
|------------|-------|
| `{{ index .Metadata.FileMatch "year" }}` | `2026` |
| `{{ index .Metadata.FileMatch "month" }}` | `01` |

## Template Functions

### Built-in Functions

| Function | Description | Example |
|----------|-------------|---------|
| `now` | Returns current time | `{{ now }}` |
| `json` | Marshals value to JSON | `{{ json .Record }}` |
| `formatTime` | Formats time with layout | `{{ formatTime .Metadata.ProcessedAt "2006-01-02" }}` |

### Path Functions

| Function | Description | Example |
|----------|-------------|---------|
| `pathDir` | Get directory of path | `{{ pathDir .Metadata.BlobName }}` |
| `pathBase` | Get base filename | `{{ pathBase .Metadata.BlobName }}` |
| `pathExt` | Get extension (no dot) | `{{ pathExt .Metadata.BlobName }}` |
| `pathSplit` | Split path into array | `{{ pathSplit .Metadata.BlobName }}` |
| `pathPart` | Get specific path component | `{{ pathPart .Metadata.PathParts 0 }}` |
| `pathJoin` | Join path components | `{{ pathJoin .Metadata.Directory "new.json" }}` |

## Examples

### Basic Enrichment

Add source metadata to each record:

```yaml
enrichment_template: |
  {
    "_source": {
      "blob": "{{ .Metadata.BlobName }}",
      "container": "{{ .Metadata.ContainerName }}",
      "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}"
    }
  }
```

**Input record:**
```json
{"message": "Hello, World!"}
```

**Output record:**
```json
{
  "message": "Hello, World!",
  "_source": {
    "blob": "logs/app/data.json",
    "container": "diagnostics",
    "processed_at": "2026-01-11T12:00:00Z"
  }
}
```

### Complete Metadata

Include all available metadata:

```yaml
enrichment_template: |
  {
    "_meta": {
      "blob": "{{ .Metadata.BlobName }}",
      "container": "{{ .Metadata.ContainerName }}",
      "storage_account": "{{ .Metadata.StorageAccount }}",
      "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}",
      "last_modified": "{{ .Metadata.LastModified.Format "2006-01-02T15:04:05Z07:00" }}",
      "size": {{ .Metadata.Size }},
      "content_type": "{{ .Metadata.ContentType }}",
      "directory": "{{ .Metadata.Directory }}",
      "filename": "{{ .Metadata.FileName }}",
      "extension": "{{ .Metadata.Extension }}"
    }
  }
```

### Hierarchical Path Extraction

Extract components from blob paths like `year/month/day/service/file.json`:

```yaml
enrichment_template: |
  {
    "_routing": {
      "year": "{{ index .Metadata.PathParts 0 }}",
      "month": "{{ index .Metadata.PathParts 1 }}",
      "day": "{{ index .Metadata.PathParts 2 }}",
      "service": "{{ index .Metadata.PathParts 3 }}",
      "directory": "{{ .Metadata.Directory }}",
      "filename": "{{ .Metadata.FileName }}"
    }
  }
```

**For blob:** `2026/01/11/myservice/events.json`

**Output:**
```json
{
  "_routing": {
    "year": "2026",
    "month": "01",
    "day": "11",
    "service": "myservice",
    "directory": "2026/01/11/myservice",
    "filename": "events.json"
  }
}
```

### Using Path Functions

Dynamic path manipulation:

```yaml
enrichment_template: |
  {
    "_file": {
      "original": "{{ .Metadata.BlobName }}",
      "directory": "{{ pathDir .Metadata.BlobName }}",
      "name": "{{ pathBase .Metadata.BlobName }}",
      "extension": "{{ pathExt .Metadata.BlobName }}",
      "first_segment": "{{ pathPart .Metadata.PathParts 0 }}",
      "archive_path": "{{ pathJoin "archive" .Metadata.Directory .Metadata.FileName }}"
    }
  }
```

### Using FileMatch with Named Capture Groups

When your parser's `file_pattern` contains named capture groups, you can access them via `.Metadata.FileMatch`:

**Parser configuration:**
```yaml
parsers:
  - id: "dated-logs"
    type: "ndjson"
    file_pattern: "logs/(?P<environment>[^/]+)/(?P<year>\\d{4})/(?P<month>\\d{2})/(?P<day>\\d{2})/(?P<service>[^/]+)/.*\\.json$"
```

**Enrichment template:**
```yaml
enrichment_template: |
  {
    "_routing": {
      "environment": "{{ index .Metadata.FileMatch "environment" }}",
      "date": "{{ index .Metadata.FileMatch "year" }}-{{ index .Metadata.FileMatch "month" }}-{{ index .Metadata.FileMatch "day" }}",
      "service": "{{ index .Metadata.FileMatch "service" }}"
    }
  }
```

**For blob:** `logs/production/2026/01/12/api-gateway/events.json`

**Output:**
```json
{
  "_routing": {
    "environment": "production",
    "date": "2026-01-12",
    "service": "api-gateway"
  }
}
```

This is particularly useful for:
- Extracting date components from path-based organization
- Identifying environments, services, or regions from blob paths
- Building dynamic routing or indexing keys

### Conditional Enrichment

Use Go template conditionals:

```yaml
enrichment_template: |
  {
    "_type": "{{ if eq .Metadata.Extension "json" }}json{{ else if eq .Metadata.Extension "csv" }}csv{{ else }}unknown{{ end }}",
    "_priority": {{ if gt .Metadata.Size 1000000 }}1{{ else }}2{{ end }}
  }
```

### Accessing Original Record Data

Reference fields from the original record:

```yaml
enrichment_template: |
  {
    "_enriched": {
      "original_timestamp": "{{ index .Record "timestamp" }}",
      "processed_at": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05Z07:00" }}",
      "source": "{{ .Metadata.BlobName }}"
    }
  }
```

### Elasticsearch-Compatible Output

Format for Elasticsearch ingestion:

```yaml
enrichment_template: |
  {
    "@timestamp": "{{ .Metadata.ProcessedAt.Format "2006-01-02T15:04:05.000Z" }}",
    "@metadata": {
      "beat": "adda",
      "type": "log"
    },
    "agent": {
      "name": "adda",
      "type": "azure-diagnostic-data-adapter"
    },
    "input": {
      "type": "azure_blob"
    },
    "azure": {
      "storage_account": "{{ .Metadata.StorageAccount }}",
      "container": "{{ .Metadata.ContainerName }}",
      "blob": {
        "name": "{{ .Metadata.BlobName }}",
        "path": "{{ .Metadata.Directory }}",
        "size": {{ .Metadata.Size }}
      }
    }
  }
```

## Best Practices

1. **Always produce valid JSON** - Test your templates with `--dry-run`

2. **Use unique field names** - Prefix enriched fields with `_` to avoid conflicts

3. **Handle missing fields** - Use `{{ if }}` for optional data

4. **Use time formatting** - Always format `time.Time` values explicitly

5. **Escape special characters** - Template output must be valid JSON

## Troubleshooting

### Invalid JSON Output

If your enrichment produces invalid JSON, you'll see errors during processing. Use `--dry-run` to preview:

```bash
adda --config config.yaml --dry-run
```

### Missing Fields

If a field is missing, the template will produce an empty string. Use conditionals to handle this:

```yaml
enrichment_template: |
  {
    "field": {{ if .Record.optional }}"{{ .Record.optional }}"{{ else }}null{{ end }}
  }
```

### Time Formatting

Go uses a reference time for formatting: `Mon Jan 2 15:04:05 MST 2006`

Common formats:
- ISO8601: `"2006-01-02T15:04:05Z07:00"`
- Date only: `"2006-01-02"`
- Time only: `"15:04:05"`
- RFC3339: `"2006-01-02T15:04:05Z07:00"`

