package main

import (
	"fmt"
	"githuboutput"
	"os"
	"os/exec"
	"parsepaths"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// This program checks for changes in translation files and writes the result to GitHub Actions output.
// It supports both flat and nested naming conventions and applies exclusion rules based on environment variables.

func main() {
	changed, err := detectChangedFiles()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error detecting changes:", err)
		os.Exit(1)
	}

	outputValue := "false"
	if changed {
		outputValue = "true"
	}

	// Write the result to GitHub Actions output
	if !githuboutput.WriteToGitHubOutput("has_changes", outputValue) {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output.")
		os.Exit(1)
	}
}

// detectChangedFiles checks for changes in translation files and applies exclusion rules.
// It returns true if any files have changed after applying the exclusion patterns.
func detectChangedFiles() (bool, error) {
	// Load and parse environment variables
	translationsPath := os.Getenv("TRANSLATIONS_PATH")
	paths := parsepaths.ParsePaths(translationsPath)
	if len(paths) == 0 {
		return false, fmt.Errorf("no valid paths found in TRANSLATIONS_PATH")
	}

	fileFormat := os.Getenv("FILE_FORMAT")
	if fileFormat == "" {
		return false, fmt.Errorf("FILE_FORMAT environment variable is required")
	}

	flatNaming, err := parseBoolEnv("FLAT_NAMING")
	if err != nil {
		return false, fmt.Errorf("invalid FLAT_NAMING value: %v", err)
	}

	alwaysPullBase, err := parseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return false, fmt.Errorf("invalid ALWAYS_PULL_BASE value: %v", err)
	}

	baseLang := os.Getenv("BASE_LANG")
	if baseLang == "" {
		return false, fmt.Errorf("BASE_LANG environment variable is required")
	}

	// Get changed and untracked files
	statusFiles, err := gitDiff(paths, fileFormat, flatNaming)
	if err != nil {
		return false, fmt.Errorf("error detecting changed files: %v", err)
	}

	untrackedFiles, err := gitLsFiles(paths, fileFormat, flatNaming)
	if err != nil {
		return false, fmt.Errorf("error detecting untracked files: %v", err)
	}

	// Combine and deduplicate files
	allChangedFiles := deduplicateFiles(statusFiles, untrackedFiles)

	// Build exclusion patterns
	excludePatterns := buildExcludePatterns(paths, baseLang, fileFormat, flatNaming, alwaysPullBase)

	// Filter the files based on the exclusion patterns
	filteredFiles := filterFiles(allChangedFiles, excludePatterns)

	// Return true if any files remain after filtering
	return len(filteredFiles) > 0, nil
}

// parseBoolEnv parses a boolean environment variable.
// Returns false if the variable is not set or empty.
// Returns an error if the value cannot be parsed as a boolean.
func parseBoolEnv(envVar string) (bool, error) {
	val := os.Getenv(envVar)
	if val == "" {
		return false, nil // Default to false if not set
	}
	return strconv.ParseBool(val)
}

// gitDiff runs `git diff --name-only HEAD -- <patterns>` to detect modified files.
func gitDiff(paths []string, fileFormat string, flatNaming bool) ([]string, error) {
	args := buildGitStatusArgs(paths, fileFormat, flatNaming, "diff", "--name-only", "HEAD")
	return runGitCommand(args)
}

// gitLsFiles runs `git ls-files --others --exclude-standard -- <patterns>` to detect untracked files.
func gitLsFiles(paths []string, fileFormat string, flatNaming bool) ([]string, error) {
	args := buildGitStatusArgs(paths, fileFormat, flatNaming, "ls-files", "--others", "--exclude-standard")
	return runGitCommand(args)
}

// buildGitStatusArgs constructs git command arguments based on the naming convention and paths.
// It builds glob patterns to match translation files.
func buildGitStatusArgs(paths []string, fileFormat string, flatNaming bool, gitCmd ...string) []string {
	var patterns []string
	for _, path := range paths {
		var pattern string
		if flatNaming {
			// For flat naming, match files like "path/*.fileFormat"
			pattern = filepath.Join(path, fmt.Sprintf("*.%s", fileFormat))
		} else {
			// For nested directories, match files like "path/**/*.fileFormat"
			pattern = filepath.Join(path, "**", fmt.Sprintf("*.%s", fileFormat))
		}
		patterns = append(patterns, pattern)
	}
	args := append(gitCmd, "--")
	args = append(args, patterns...)
	return args
}

// runGitCommand executes a git command and returns the output lines.
// If the command exits with a non-zero status, it returns an error.
func runGitCommand(args []string) ([]string, error) {
	cmd := exec.Command("git", args...)
	outputBytes, err := cmd.Output()
	if err != nil {
		return nil, err // Return error if git command fails
	}

	outputStr := strings.TrimSpace(string(outputBytes))
	if outputStr == "" {
		return nil, nil // No output means no files changed
	}

	lines := strings.Split(outputStr, "\n")
	var results []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			results = append(results, line)
		}
	}
	return results, nil
}

// deduplicateFiles combines and deduplicates two slices of files.
func deduplicateFiles(statusFiles, untrackedFiles []string) []string {
	fileSet := make(map[string]struct{})
	for _, file := range statusFiles {
		fileSet[file] = struct{}{}
	}
	for _, file := range untrackedFiles {
		fileSet[file] = struct{}{}
	}
	// Collect the keys from the map to get the deduplicated list
	allFiles := make([]string, 0, len(fileSet))
	for file := range fileSet {
		allFiles = append(allFiles, file)
	}
	sort.Strings(allFiles)
	return allFiles
}

// buildExcludePatterns builds exclusion patterns based on settings.
// Returns a slice of regular expression strings.
func buildExcludePatterns(paths []string, baseLang, fileFormat string, flatNaming, alwaysPullBase bool) []string {
	var excludePatterns []string
	for _, path := range paths {
		if flatNaming {
			if !alwaysPullBase {
				// Exclude the base language file, e.g., "path/baseLang.fileFormat"
				baseLangFile := filepath.Join(path, fmt.Sprintf("%s.%s", baseLang, fileFormat))
				pattern := fmt.Sprintf("^%s$", regexp.QuoteMeta(baseLangFile))
				excludePatterns = append(excludePatterns, pattern)
			}
			// Exclude any files in subdirectories under the path
			// Pattern matches files under at least one subdirectory: "^path/.+/.+"
			pattern := fmt.Sprintf("^%s%s.+%s.+", regexp.QuoteMeta(path), regexp.QuoteMeta(string(os.PathSeparator)), regexp.QuoteMeta(string(os.PathSeparator)))
			excludePatterns = append(excludePatterns, pattern)
		} else {
			if !alwaysPullBase {
				// Exclude any files under "path/baseLang/"
				baseLangDir := filepath.Join(path, baseLang) + string(os.PathSeparator)
				pattern := fmt.Sprintf("^%s.*", regexp.QuoteMeta(baseLangDir))
				excludePatterns = append(excludePatterns, pattern)
			}
		}
	}
	return excludePatterns
}

// filterFiles filters the list of files based on exclusion patterns.
func filterFiles(files, excludePatterns []string) []string {
	if len(excludePatterns) == 0 {
		return files // No exclusion patterns; return all files
	}
	// Combine all exclusion patterns into a single regular expression
	excludeRegex := regexp.MustCompile(strings.Join(excludePatterns, "|"))
	var filtered []string
	for _, file := range files {
		if !excludeRegex.MatchString(file) {
			filtered = append(filtered, file) // Include files that do not match exclusion patterns
		}
	}
	return filtered
}
