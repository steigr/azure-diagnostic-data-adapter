// Package external provides an external command parser implementation.
package external

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/steigr/azure-diagnostic-data-adapter/pkg/parser"
)

// Parser runs an external command to parse data.
type Parser struct {
	*parser.BaseParser
	command string
	args    []string
	env     map[string]string
	timeout time.Duration
	tempDir string
	stdin   bool
	stdout  bool
}

// Config holds configuration for the external parser.
type Config struct {
	ID          string
	FilePattern string
	Command     string
	Args        []string
	Env         map[string]string
	Timeout     time.Duration
	TempDir     string
	Stdin       bool // If true, input is provided via stdin instead of INPUT_FILE
	Stdout      bool // If true, output is read from stdout instead of OUTPUT_FILE
}

// New creates a new external parser.
func New(cfg Config) (*Parser, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.TempDir == "" {
		cfg.TempDir = os.TempDir()
	}
	base, err := parser.NewBaseParser(cfg.ID, cfg.FilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid file pattern: %w", err)
	}
	return &Parser{
		BaseParser: base,
		command:    cfg.Command,
		args:       cfg.Args,
		env:        cfg.Env,
		timeout:    cfg.Timeout,
		tempDir:    cfg.TempDir,
		stdin:      cfg.Stdin,
		stdout:     cfg.Stdout,
	}, nil
}

// Parse runs the external command with input data and returns parsed records.
// Depending on configuration, input/output can be via files or stdin/stdout.
// The output is expected to be in NDJSON format (one JSON object per line).
func (p *Parser) Parse(input io.Reader) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	// Create unique temporary directory for this parsing operation
	tempDir := filepath.Join(p.tempDir, fmt.Sprintf("adda-%s", uuid.New().String()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Build environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("TEMP_DIR=%s", tempDir))

	var inputPath, outputPath string

	// Handle input: stdin or file
	if !p.stdin {
		// Write input to file
		inputPath = filepath.Join(tempDir, "input")
		inputFile, err := os.Create(inputPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create temp input file: %w", err)
		}

		if _, err := io.Copy(inputFile, input); err != nil {
			_ = inputFile.Close()
			return nil, fmt.Errorf("failed to write to temp input file: %w", err)
		}
		if err := inputFile.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temp input file: %w", err)
		}
		env = append(env, fmt.Sprintf("INPUT_FILE=%s", inputPath))
	}

	// Handle output: stdout or file
	if !p.stdout {
		outputPath = filepath.Join(tempDir, "output.ndjson")
		env = append(env, fmt.Sprintf("OUTPUT_FILE=%s", outputPath))
	}

	for k, v := range p.env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Run external command
	cmd := exec.CommandContext(ctx, p.command, p.args...)
	cmd.Env = env

	var outputBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	if p.stdin {
		cmd.Stdin = input
	}

	if p.stdout {
		cmd.Stdout = &outputBuf
		cmd.Stderr = &stderrBuf
	}

	var err error
	var combinedOutput []byte

	if p.stdout {
		err = cmd.Run()
		if err != nil {
			stderrStr := strings.TrimSpace(stderrBuf.String())
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("external command timed out after %v: %w", p.timeout, err)
			}
			return nil, fmt.Errorf("external command failed: %w, stderr: %s", err, stderrStr)
		}
	} else {
		combinedOutput, err = cmd.CombinedOutput()
		if err != nil {
			outputStr := strings.TrimSpace(string(combinedOutput))
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("external command timed out after %v: %w", p.timeout, err)
			}
			return nil, fmt.Errorf("external command failed: %w, output: %s", err, outputStr)
		}
	}

	// Read output data
	var outputData []byte
	if p.stdout {
		outputData = outputBuf.Bytes()
	} else {
		outputData, err = os.ReadFile(outputPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("external command did not create output file at %s", outputPath)
			}
			return nil, fmt.Errorf("failed to read output file: %w", err)
		}
	}

	if len(outputData) == 0 {
		return nil, nil
	}

	// Parse NDJSON output (one JSON object per line)
	return parseNDJSON(outputData)
}

// parseNDJSON parses NDJSON formatted data (one JSON object per line).
func parseNDJSON(data []byte) ([]map[string]any, error) {
	var records []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("failed to parse NDJSON at line %d: %w", lineNum, err)
		}
		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading NDJSON: %w", err)
	}

	return records, nil
}
