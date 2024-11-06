package main

import (
	"bufio"
	"fmt"
	"githuboutput"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Main function to detect changes and write to GitHub output
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

	if !githuboutput.WriteToGitHubOutput("has_changes", outputValue) {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output.")
		os.Exit(1)
	}
}

// Detects changed files in specified paths and applies exclusion rules
func detectChangedFiles() (bool, error) {
	// Load environment variables
	paths, err := parsePaths(os.Getenv("TRANSLATIONS_PATH"))
	if err != nil || len(paths) == 0 {
		return false, fmt.Errorf("no valid paths found or error parsing paths: %v", err)
	}

	fileFormat := os.Getenv("FILE_FORMAT")
	flatNaming := os.Getenv("FLAT_NAMING") == "true"
	alwaysPullBase := os.Getenv("ALWAYS_PULL_BASE") == "true"
	baseLang := os.Getenv("BASE_LANG")

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
	filteredFiles := filterFiles(allChangedFiles, excludePatterns)

	// Return true if any files remain after filtering
	return len(filteredFiles) > 0, nil
}

// Parses paths from TRANSLATIONS_PATH environment variable
func parsePaths(translationsPath string) ([]string, error) {
	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(translationsPath))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path != "" {
			paths = append(paths, path)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading paths: %v", err)
	}
	return paths, nil
}

// Runs `git diff --name-only` to detect modified files
func gitDiff(paths []string, fileFormat string, flatNaming bool) ([]string, error) {
	args := buildGitStatusArgs(paths, fileFormat, flatNaming, "diff", "--name-only", "HEAD")
	return runGitCommand(args)
}

// Runs `git ls-files` to detect untracked files
func gitLsFiles(paths []string, fileFormat string, flatNaming bool) ([]string, error) {
	args := buildGitStatusArgs(paths, fileFormat, flatNaming, "ls-files", "--others", "--exclude-standard")
	return runGitCommand(args)
}

// Builds git command arguments based on naming convention
func buildGitStatusArgs(paths []string, fileFormat string, flatNaming bool, gitCmd ...string) []string {
	var patterns []string
	for _, path := range paths {
		if flatNaming {
			patterns = append(patterns, fmt.Sprintf("%s/*.%s", path, fileFormat))
		} else {
			patterns = append(patterns, fmt.Sprintf("%s/**/*.%s", path, fileFormat))
		}
	}
	return append(gitCmd, append([]string{"--"}, patterns...)...)
}

// Executes a git command and returns the output as a slice of strings
func runGitCommand(args []string) ([]string, error) {
	cmd := exec.Command("git", args...)
	outputBytes, err := cmd.Output()
	if err != nil && err.Error() != "exit status 1" {
		return nil, err
	}
	// Split the output into lines and remove any empty strings
	output := strings.Split(strings.TrimSpace(string(outputBytes)), "\n")
	var results []string
	for _, line := range output {
		if line != "" {
			results = append(results, line)
		}
	}
	return results, nil
}

// Deduplicates and combines two slices of files
func deduplicateFiles(statusFiles, untrackedFiles []string) []string {
	fileSet := make(map[string]struct{})
	for _, file := range append(statusFiles, untrackedFiles...) {
		if file != "" {
			fileSet[file] = struct{}{}
		}
	}
	var allFiles []string
	for file := range fileSet {
		allFiles = append(allFiles, file)
	}
	sort.Strings(allFiles)
	return allFiles
}

// Builds exclusion patterns based on settings
func buildExcludePatterns(paths []string, baseLang, fileFormat string, flatNaming, alwaysPullBase bool) []string {
	var excludePatterns []string
	for _, path := range paths {
		if flatNaming {
			if !alwaysPullBase {
				pattern := fmt.Sprintf("^%s/%s\\.%s$", regexp.QuoteMeta(path), regexp.QuoteMeta(baseLang), regexp.QuoteMeta(fileFormat))
				excludePatterns = append(excludePatterns, pattern)
			}
			pattern := fmt.Sprintf("^%s/.+/.*$", regexp.QuoteMeta(path))
			excludePatterns = append(excludePatterns, pattern)
		} else if !alwaysPullBase {
			pattern := fmt.Sprintf("^%s/%s/", regexp.QuoteMeta(path), regexp.QuoteMeta(baseLang))
			excludePatterns = append(excludePatterns, pattern)
		}
	}
	return excludePatterns
}

// Filters files based on exclusion patterns
func filterFiles(files, excludePatterns []string) []string {
	if len(excludePatterns) == 0 {
		return files
	}
	excludeRegex := regexp.MustCompile(strings.Join(excludePatterns, "|"))
	var filtered []string
	for _, file := range files {
		if !excludeRegex.MatchString(file) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}
