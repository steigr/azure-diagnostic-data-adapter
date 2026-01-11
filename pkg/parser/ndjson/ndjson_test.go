package ndjson

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
		{"default id", "", "ndjson"},
		{"custom id", "custom-ndjson", "custom-ndjson"},
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

func TestParser_Parse_NDJSON(t *testing.T) {
	input := `{"name":"line1","value":1}
{"name":"line2","value":2}
{"name":"line3","value":3}`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 3 {
		t.Errorf("Parse() returned %d records, want 3", len(records))
	}

	if records[0]["name"] != "line1" {
		t.Errorf("records[0][name] = %v, want line1", records[0]["name"])
	}
	if records[1]["name"] != "line2" {
		t.Errorf("records[1][name] = %v, want line2", records[1]["name"])
	}
	if records[2]["name"] != "line3" {
		t.Errorf("records[2][name] = %v, want line3", records[2]["name"])
	}
}

func TestParser_Parse_SingleLine(t *testing.T) {
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

func TestParser_Parse_WithEmptyLines(t *testing.T) {
	input := `{"name":"line1","value":1}

{"name":"line2","value":2}
   
{"name":"line3","value":3}
`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(records) != 3 {
		t.Errorf("Parse() returned %d records, want 3", len(records))
	}
}

func TestParser_Parse_WithInvalidLines(t *testing.T) {
	// One valid line, one invalid, one valid - should skip the invalid line
	input := `{"name":"line1","value":1}
this is not valid json
{"name":"line3","value":3}`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should get 2 valid records, skipping the invalid line
	if len(records) != 2 {
		t.Errorf("Parse() returned %d records, want 2", len(records))
	}

	if records[0]["name"] != "line1" {
		t.Errorf("records[0][name] = %v, want line1", records[0]["name"])
	}
	if records[1]["name"] != "line3" {
		t.Errorf("records[1][name] = %v, want line3", records[1]["name"])
	}
}

func TestParser_Parse_AllInvalidLines(t *testing.T) {
	// All invalid lines - should return empty
	input := `not valid json
also not valid
still not valid`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should return 0 records (all lines were invalid)
	if len(records) != 0 {
		t.Errorf("Parse() returned %d records, want 0", len(records))
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

func TestParser_Parse_MixedValidInvalid(t *testing.T) {
	// Test with a more complex mix of valid/invalid data
	input := `{"id":1,"message":"first"}
{"id":2,"message":"second"}
invalid json here {
{"id":3,"message":"third"}
another invalid line
{"id":4,"message":"fourth"}`

	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should get 4 valid records
	if len(records) != 4 {
		t.Errorf("Parse() returned %d records, want 4", len(records))
	}

	// Verify the records are in order
	expectedIDs := []float64{1, 2, 3, 4}
	for i, expectedID := range expectedIDs {
		if records[i]["id"] != expectedID {
			t.Errorf("records[%d][id] = %v, want %v", i, records[i]["id"], expectedID)
		}
	}
}

func TestParser_Parse_LongLines(t *testing.T) {
	// Test with lines that have more content
	input := `{"event":"login","user":"alice@example.com","timestamp":"2026-01-11T10:00:00Z","metadata":{"ip":"192.168.1.1","user_agent":"Mozilla/5.0"}}
{"event":"logout","user":"bob@example.com","timestamp":"2026-01-11T10:30:00Z","metadata":{"ip":"192.168.1.2","user_agent":"Chrome/120.0"}}`

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

	if records[0]["event"] != "login" {
		t.Errorf("records[0][event] = %v, want login", records[0]["event"])
	}
	if records[1]["event"] != "logout" {
		t.Errorf("records[1][event] = %v, want logout", records[1]["event"])
	}
}

func TestParser_Parse_NestedObjects(t *testing.T) {
	input := `{"level":"info","msg":"test","nested":{"key":"value","array":[1,2,3]}}
{"level":"error","msg":"failure","nested":{"error_code":500,"details":{"reason":"timeout"}}}`

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

	// Check nested structure is preserved
	nested, ok := records[0]["nested"].(map[string]any)
	if !ok {
		t.Fatal("records[0][nested] is not a map")
	}
	if nested["key"] != "value" {
		t.Errorf("records[0][nested][key] = %v, want value", nested["key"])
	}
}

func TestParser_Parse_JSONArrayNotSupported(t *testing.T) {
	// NDJSON parser should NOT parse JSON arrays as valid records
	// It treats each line as a separate JSON object, so an array on one line
	// would be parsed but not split
	input := `[{"name":"test1"},{"name":"test2"}]`
	p, err := New("", "")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	records, err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Should return 0 records because the line is a JSON array, not an object
	if len(records) != 0 {
		t.Errorf("Parse() returned %d records, want 0 (JSON arrays are not valid NDJSON lines)", len(records))
	}
}
