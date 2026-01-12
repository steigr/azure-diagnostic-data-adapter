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
// This is a convenience method that calls ParseWithContext with an empty context.
func (p *Parser) Parse(input io.Reader) ([]map[string]any, error) {
	return p.ParseWithContext(input, parser.ParseContext{})
}

// ParseWithContext runs the external command with input data and context, returning parsed records.
// Depending on configuration, input/output can be via files or stdin/stdout.
// The output is expected to be in NDJSON format (one JSON object per line).
// The context provides storage account and container name information as environment variables.
func (p *Parser) ParseWithContext(input io.Reader, parseCtx parser.ParseContext) ([]map[string]any, error) {
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

	// Build base environment
	env := os.Environ()
	env = append(env, fmt.Sprintf("TEMP_DIR=%s", tempDir))

	// Add storage account and container name as environment variables
	if parseCtx.StorageAccountName != "" {
		env = append(env, fmt.Sprintf("STORAGE_ACCOUNT_NAME=%s", parseCtx.StorageAccountName))
	}
	if parseCtx.ContainerName != "" {
		env = append(env, fmt.Sprintf("CONTAINER_NAME=%s", parseCtx.ContainerName))
	}
	if parseCtx.BlobName != "" {
		env = append(env, fmt.Sprintf("BLOB_NAME=%s", parseCtx.BlobName))
	}

	// Add file match capture groups as environment variables with MATCH_ prefix
	for name, value := range parseCtx.FileMatch {
		envName := fmt.Sprintf("MATCH_%s", strings.ToUpper(name))
		env = append(env, fmt.Sprintf("%s=%s", envName, value))
	}

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

	// Add user-defined environment variables with envsubst interpolation
	for k, v := range p.env {
		// Perform environment variable interpolation on the value
		interpolatedValue := envsubst(v, env)
		env = append(env, fmt.Sprintf("%s=%s", k, interpolatedValue))
	}

	// Resolve command: first envsubst, then glob pattern expansion
	command := envsubst(p.command, env)
	resolvedCommand, err := resolveGlob(command)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve command glob pattern: %w", err)
	}
	if resolvedCommand != "" {
		command = resolvedCommand
	}

	// Resolve args: first envsubst, then glob pattern expansion
	var args []string
	for _, arg := range p.args {
		// First interpolate environment variables
		interpolatedArg := envsubst(arg, env)
		// Then try glob expansion
		resolvedArg, err := resolveGlob(interpolatedArg)
		if err != nil {
			// If glob fails, use the interpolated arg
			args = append(args, interpolatedArg)
			continue
		}
		if resolvedArg != "" {
			args = append(args, resolvedArg)
		} else {
			args = append(args, interpolatedArg)
		}
	}

	// Run external command
	cmd := exec.CommandContext(ctx, command, args...)
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
		combinedOutput, err := cmd.CombinedOutput()
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

// envsubst performs environment variable substitution similar to the envsubst command.
// It replaces ${VAR} and $VAR patterns with their values from the provided environment.
func envsubst(s string, env []string) string {
	// Build environment map for lookup
	envMap := make(map[string]string)
	for _, e := range env {
		if idx := strings.Index(e, "="); idx > 0 {
			envMap[e[:idx]] = e[idx+1:]
		}
	}

	result := s

	// Replace ${VAR} patterns
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		varName := result[start+2 : end]
		varValue := envMap[varName]
		result = result[:start] + varValue + result[end+1:]
	}

	// Replace $VAR patterns (without braces)
	// Process from right to left to handle overlapping patterns correctly
	words := strings.Fields(result)
	for i, word := range words {
		if strings.HasPrefix(word, "$") && !strings.HasPrefix(word, "${") {
			varName := strings.TrimPrefix(word, "$")
			// Remove any trailing non-alphanumeric characters
			varEnd := 0
			for j, c := range varName {
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
					varEnd = j + 1
				} else {
					break
				}
			}
			if varEnd > 0 {
				actualVarName := varName[:varEnd]
				suffix := varName[varEnd:]
				if val, ok := envMap[actualVarName]; ok {
					words[i] = val + suffix
				}
			}
		}
	}

	return strings.Join(words, " ")
}

// resolveGlob attempts to resolve a glob pattern to a single matching file.
// Returns the first match if the pattern contains glob characters, otherwise returns empty string.
func resolveGlob(pattern string) (string, error) {
	// Check if pattern contains glob characters
	if !strings.ContainsAny(pattern, "*?[") {
		return "", nil
	}

	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	// Return the first match
	return matches[0], nil
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
