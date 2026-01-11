package utils

import (
	"os"
	"testing"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		want    bool
		wantErr bool
	}{
		{"simple match", "test", "test", true, false},
		{"no match", "test", "other", false, false},
		{"regex match", `.*\.json$`, "file.json", true, false},
		{"regex no match", `.*\.json$`, "file.txt", false, false},
		{"invalid regex", `[`, "test", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MatchesPattern(tt.pattern, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("MatchesPattern() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("MatchesPattern() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompilePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"valid pattern", `.*\.json$`, false},
		{"invalid pattern", `[`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompilePattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("CompilePattern() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetFreeSpace(t *testing.T) {
	// Test with current directory
	space, err := GetFreeSpace(".")
	if err != nil {
		t.Fatalf("GetFreeSpace() error = %v", err)
	}

	if space == 0 {
		t.Error("GetFreeSpace() returned 0, expected > 0")
	}

	// Test with non-existent path
	_, err = GetFreeSpace("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("GetFreeSpace() expected error for non-existent path")
	}
}

func TestGetFreeSpace_TempDir(t *testing.T) {
	dir := os.TempDir()
	space, err := GetFreeSpace(dir)
	if err != nil {
		t.Fatalf("GetFreeSpace(%s) error = %v", dir, err)
	}

	// Should have at least some space
	if space == 0 {
		t.Errorf("GetFreeSpace(%s) = 0, expected > 0", dir)
	}
}

func TestMinInt(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		if got := MinInt(tt.a, tt.b); got != tt.want {
			t.Errorf("MinInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMaxInt(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 2},
		{2, 1, 2},
		{5, 5, 5},
		{-1, 1, 1},
	}

	for _, tt := range tests {
		if got := MaxInt(tt.a, tt.b); got != tt.want {
			t.Errorf("MaxInt(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
