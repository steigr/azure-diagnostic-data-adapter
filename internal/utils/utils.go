// Package utils provides utility functions for the Azure Diagnostic Data Reader.
package utils

import (
	"regexp"
	"syscall"
)

// MatchesPattern checks if a string matches a given regex pattern.
func MatchesPattern(pattern, s string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(s), nil
}

// CompilePattern compiles a regex pattern and returns the compiled regex.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern)
}

// GetFreeSpace returns the available free space in bytes for the given path.
func GetFreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Available blocks * block size
	return stat.Bavail * uint64(stat.Bsize), nil
}

// MinInt returns the minimum of two integers.
func MinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaxInt returns the maximum of two integers.
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
