package parser

import (
	"io"
	"testing"
)

// mockParser is a simple parser for testing
type mockParser struct {
	*BaseParser
}

func (m *mockParser) Parse(input io.Reader) ([]map[string]any, error) {
	return nil, nil
}

func newMockParser(id, pattern string) (*mockParser, error) {
	base, err := NewBaseParser(id, pattern)
	if err != nil {
		return nil, err
	}
	return &mockParser{BaseParser: base}, nil
}

func TestBaseParser_Matches(t *testing.T) {
	tests := []struct {
		name        string
		filePattern string
		blobName    string
		want        bool
	}{
		{"no pattern matches all", "", "anything.txt", true},
		{"json pattern matches json", `.*\.json$`, "data.json", true},
		{"json pattern no match txt", `.*\.json$`, "data.txt", false},
		{"complex path pattern", `logs/\d{4}/.*\.json$`, "logs/2024/app.json", true},
		{"complex path no match", `logs/\d{4}/.*\.json$`, "logs/app.json", false},
		// Case-insensitive matching tests
		{"uppercase extension matches lowercase pattern", `.*\.json$`, "data.JSON", true},
		{"mixed case extension matches", `.*\.json$`, "data.Json", true},
		{"uppercase path matches lowercase pattern", `logs/.*\.json$`, "LOGS/data.json", true},
		{"mixed case path matches", `logs/.*\.json$`, "Logs/Data.JSON", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, err := NewBaseParser("test", tt.filePattern)
			if err != nil {
				t.Fatalf("NewBaseParser() error = %v", err)
			}
			if got := base.Matches(tt.blobName); got != tt.want {
				t.Errorf("Matches(%s) = %v, want %v", tt.blobName, got, tt.want)
			}
		})
	}
}

func TestBaseParser_InvalidPattern(t *testing.T) {
	_, err := NewBaseParser("test", "[invalid")
	if err == nil {
		t.Error("NewBaseParser() expected error for invalid pattern")
	}
}

func TestRegistry_FindMatching(t *testing.T) {
	registry := NewRegistry()

	jsonParser, _ := newMockParser("json", `.*\.json$`)
	csvParser, _ := newMockParser("csv", `.*\.csv$`)
	defaultParser, _ := newMockParser("default", "")

	registry.Register(jsonParser)
	registry.Register(csvParser)
	registry.Register(defaultParser)

	tests := []struct {
		name      string
		blobName  string
		wantID    string
		wantFound bool
	}{
		{"matches json", "data.json", "json", true},
		{"matches csv", "data.csv", "csv", true},
		{"falls back to default", "data.txt", "default", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, found := registry.FindMatching(tt.blobName)
			if found != tt.wantFound {
				t.Errorf("FindMatching(%s) found = %v, want %v", tt.blobName, found, tt.wantFound)
			}
			if found && p.ID() != tt.wantID {
				t.Errorf("FindMatching(%s) ID = %v, want %v", tt.blobName, p.ID(), tt.wantID)
			}
		})
	}
}

func TestRegistry_FindMatching_NoMatch(t *testing.T) {
	registry := NewRegistry()

	jsonParser, _ := newMockParser("json", `.*\.json$`)
	registry.Register(jsonParser)

	_, found := registry.FindMatching("data.txt")
	if found {
		t.Error("FindMatching() should return false when no parser matches")
	}
}

func TestRegistry_Get(t *testing.T) {
	registry := NewRegistry()

	parser, _ := newMockParser("test", "")
	registry.Register(parser)

	p, ok := registry.Get("test")
	if !ok {
		t.Error("Get() should find registered parser")
	}
	if p.ID() != "test" {
		t.Errorf("Get() ID = %v, want test", p.ID())
	}

	_, ok = registry.Get("nonexistent")
	if ok {
		t.Error("Get() should return false for nonexistent parser")
	}
}

func TestRegistry_List(t *testing.T) {
	registry := NewRegistry()

	p1, _ := newMockParser("parser1", "")
	p2, _ := newMockParser("parser2", "")
	registry.Register(p1)
	registry.Register(p2)

	ids := registry.List()
	if len(ids) != 2 {
		t.Errorf("List() returned %d IDs, want 2", len(ids))
	}
}
