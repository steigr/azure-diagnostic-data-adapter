package json

import (
	"strings"
	"testing"
)

func TestParser_ID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected string
	}{
		{"default id", "", "json"},
		{"custom id", "custom-json", "custom-json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New(tt.id, "")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := p.ID(); got != tt.expected {
				t.Errorf("ID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParser_Matches(t *testing.T) {
	tests := []struct {
		name        string
		filePattern string
		blobName    string
		want        bool
	}{
		{"no pattern matches all", "", "anything.txt", true},
		{"json pattern matches json", `.*\.json$`, "data.json", true},
		{"json pattern no match txt", `.*\.json$`, "data.txt", false},
		{"complex pattern", `logs/.*\.json$`, "logs/app.json", true},
		{"complex pattern no match", `logs/.*\.json$`, "other/app.json", false},
		// Case-insensitive matching tests
		{"uppercase JSON extension", `.*\.json$`, "data.JSON", true},
		{"mixed case Json extension", `.*\.json$`, "data.Json", true},
		{"uppercase path", `logs/.*\.json$`, "LOGS/app.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := New("test", tt.filePattern)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := p.Matches(tt.blobName); got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.blobName, got, tt.want)
			}
		})
	}
}

func TestParser_InvalidPattern(t *testing.T) {
	_, err := New("test", "[invalid")
	if err == nil {
		t.Error("New() expected error for invalid pattern")
	}
}

func TestParser_Parse_Array(t *testing.T) {
	input := `[{"name":"test1","value":1},{"name":"test2","value":2}]`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Parse() returned %d records, want 2", len(records))
	}

	if records[0]["name"] != "test1" {
		t.Errorf("records[0][name] = %v, want test1", records[0]["name"])
	}
}

func TestParser_Parse_SingleObject(t *testing.T) {
	input := `{"name":"single","value":42}`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 1 {
		t.Errorf("Parse() returned %d records, want 1", len(records))
	}

	if records[0]["name"] != "single" {
		t.Errorf("records[0][name] = %v, want single", records[0]["name"])
	}
}

func TestParser_Parse_Empty(t *testing.T) {
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Parse() returned %d records, want 0", len(records))
	}
}

func TestParser_Parse_EmptyArray(t *testing.T) {
	input := `[]`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 0 {
		t.Errorf("Parse() returned %d records, want 0", len(records))
	}
}

func TestParser_Parse_NestedObjects(t *testing.T) {
	input := `[{"id":1,"data":{"nested":"value"}},{"id":2,"data":{"nested":"other"}}]`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Parse() returned %d records, want 2", len(records))
	}

	data, ok := records[0]["data"].(map[string]any)
	if !ok {
		t.Fatal("records[0][data] is not a map")
	}
	if data["nested"] != "value" {
		t.Errorf("records[0][data][nested] = %v, want value", data["nested"])
	}
}

func TestParser_Parse_InvalidJSON(t *testing.T) {
	input := `this is not valid json`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = p.Parse(strings.NewReader(input))
	if err == nil {
		t.Error("Parse() expected error for invalid JSON")
	}
}

func TestParser_Parse_NDJSON_NotSupported(t *testing.T) {
	// JSON parser does NOT support NDJSON - use ndjson parser for that
	input := `{"name":"line1"}
{"name":"line2"}`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = p.Parse(strings.NewReader(input))
	if err == nil {
		t.Error("Parse() expected error for NDJSON input (use ndjson parser instead)")
	}
}

func TestParser_Parse_PrettyPrintedJSON(t *testing.T) {
	input := `[
  {
    "name": "test1",
    "value": 1
  },
  {
    "name": "test2",
    "value": 2
  }
]`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 2 {
		t.Errorf("Parse() returned %d records, want 2", len(records))
	}
}

func TestParser_Parse_SinglePrettyPrintedObject(t *testing.T) {
	input := `{
  "name": "single",
  "value": 42
}`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 1 {
		t.Errorf("Parse() returned %d records, want 1", len(records))
	}

	if records[0]["name"] != "single" {
		t.Errorf("records[0][name] = %v, want single", records[0]["name"])
	}
}
