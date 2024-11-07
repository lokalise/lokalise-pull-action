package parsepaths

import (
	"strings"
)

// ParsePaths takes a newline-separated string from an environment variable
// and returns a slice of cleaned paths. It handles both Unix and Windows
// line endings and filters out any empty or whitespace-only lines.
func ParsePaths(envVar string) []string {
	// Split the input string by the newline character '\n'
	lines := strings.Split(envVar, "\n")
	var paths []string

	for _, line := range lines {
		// Trim leading and trailing whitespace, including spaces and carriage returns
		path := strings.TrimSpace(line)
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
