package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/githuboutput"

	"github.com/bodrovis/lokalise-actions-common/parsepaths"
)

// This program checks for changes in translation files and writes the result to GitHub Actions output.
// It supports both flat and nested naming conventions and applies exclusion rules based on environment variables.

type CommandRunner interface {
	Run(name string, args ...string) ([]string, error)
}

type DefaultCommandRunner struct{}

func (d DefaultCommandRunner) Run(name string, args ...string) ([]string, error) {
	cmd := exec.Command(name, args...)
	outputBytes, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(outputBytes))

	if err != nil {
		return nil, fmt.Errorf("command '%s %s' failed: %v\nOutput:\n%s", name, strings.Join(args, " "), err, outputStr)
	}

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

type Config struct {
	TranslationsPath string
	FileFormat       string
	FlatNaming       bool
	AlwaysPullBase   bool
	BaseLang         string
	Paths            []string
}

func main() {
	// Prepare configuration
	config, err := prepareConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error preparing configuration:", err)
		os.Exit(1)
	}

	// Detect changes
	changed, err := detectChangedFiles(config, DefaultCommandRunner{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error detecting changes:", err)
		os.Exit(1)
	}

	var outputValue string
	if changed {
		outputValue = "true"
		fmt.Println("Detected changes in translation files.")
	} else {
		outputValue = "false"
		fmt.Println("No changes detected in translation files.")
	}

	// Write the result to GitHub Actions output
	if !githuboutput.WriteToGitHubOutput("has_changes", outputValue) {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output.")
		os.Exit(1)
	}
}

// detectChangedFiles checks for changes in translation files and applies exclusion rules.
// It returns true if any files have changed after applying the exclusion patterns.
func detectChangedFiles(config *Config, runner CommandRunner) (bool, error) {
	// Get changed and untracked files
	statusFiles, err := gitDiff(config, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting changed files: %v", err)
	}

	untrackedFiles, err := gitLsFiles(config, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting untracked files: %v", err)
	}

	// Combine and deduplicate files
	allChangedFiles := deduplicateFiles(statusFiles, untrackedFiles)

	// Build exclusion patterns
	excludePatterns, err := buildExcludePatterns(config)
	if err != nil {
		return false, fmt.Errorf("error building exclusion patterns: %v", err)
	}

	// Filter the files based on the exclusion patterns
	filteredFiles := filterFiles(allChangedFiles, excludePatterns)

	// Return true if any files remain after filtering
	return len(filteredFiles) > 0, nil
}

// gitDiff runs `git diff --name-only HEAD -- <patterns>` to detect modified files.
func gitDiff(config *Config, runner CommandRunner) ([]string, error) {
	args := buildGitStatusArgs(config.Paths, config.FileFormat, config.FlatNaming, "diff", "--name-only", "HEAD")
	return runner.Run("git", args...)
}

// gitLsFiles runs `git ls-files --others --exclude-standard -- <patterns>` to detect untracked files.
func gitLsFiles(config *Config, runner CommandRunner) ([]string, error) {
	args := buildGitStatusArgs(config.Paths, config.FileFormat, config.FlatNaming, "ls-files", "--others", "--exclude-standard")
	return runner.Run("git", args...)
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
	slices.Sort(allFiles)
	return allFiles
}

// buildExcludePatterns builds exclusion patterns based on settings.
// Returns a slice of regular expression strings.
func buildExcludePatterns(config *Config) ([]*regexp.Regexp, error) {
	var excludePatterns []*regexp.Regexp

	for _, path := range config.Paths {
		path = filepath.ToSlash(path)

		if config.FlatNaming {
			if !config.AlwaysPullBase {
				baseLangFile := filepath.ToSlash(filepath.Join(path, fmt.Sprintf("%s.%s", config.BaseLang, config.FileFormat)))
				patternStr := fmt.Sprintf("^%s$", regexp.QuoteMeta(baseLangFile))
				pattern, err := regexp.Compile(patternStr)
				if err != nil {
					return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
				}
				excludePatterns = append(excludePatterns, pattern)
			}
			patternStr := fmt.Sprintf("^%s/[^/]+/.*", regexp.QuoteMeta(path))
			pattern, err := regexp.Compile(patternStr)
			if err != nil {
				return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
			}
			excludePatterns = append(excludePatterns, pattern)
		} else {
			if !config.AlwaysPullBase {
				baseLangDir := filepath.ToSlash(filepath.Join(path, config.BaseLang))
				patternStr := fmt.Sprintf("^%s/.*", regexp.QuoteMeta(baseLangDir))
				pattern, err := regexp.Compile(patternStr)
				if err != nil {
					return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
				}
				excludePatterns = append(excludePatterns, pattern)
			}
		}
	}
	return excludePatterns, nil
}

// filterFiles filters the list of files based on exclusion patterns.
func filterFiles(files []string, excludePatterns []*regexp.Regexp) []string {
	if len(excludePatterns) == 0 {
		return files // No exclusion patterns; return all files
	}

	var filtered []string
	for _, file := range files {
		file = filepath.ToSlash(file)
		exclude := false
		for _, pattern := range excludePatterns {
			if pattern.MatchString(file) {
				exclude = true
				break
			}
		}
		if !exclude {
			filtered = append(filtered, file)
		}
	}
	return filtered
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

func prepareConfig() (*Config, error) {
	// Parse boolean environment variables
	flatNaming, err := parseBoolEnv("FLAT_NAMING")
	if err != nil {
		return nil, fmt.Errorf("invalid FLAT_NAMING value: %v", err)
	}

	alwaysPullBase, err := parseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return nil, fmt.Errorf("invalid ALWAYS_PULL_BASE value: %v", err)
	}

	// Validate required environment variables
	translationsPath := os.Getenv("TRANSLATIONS_PATH")
	if translationsPath == "" {
		return nil, fmt.Errorf("TRANSLATIONS_PATH environment variable is required")
	}

	fileFormat := os.Getenv("FILE_FORMAT")
	if fileFormat == "" {
		return nil, fmt.Errorf("FILE_FORMAT environment variable is required")
	}

	baseLang := os.Getenv("BASE_LANG")
	if baseLang == "" {
		return nil, fmt.Errorf("BASE_LANG environment variable is required")
	}

	// Parse paths from TRANSLATIONS_PATH
	paths := parsepaths.ParsePaths(translationsPath)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no valid paths found in TRANSLATIONS_PATH")
	}

	return &Config{
		TranslationsPath: translationsPath,
		FileFormat:       fileFormat,
		FlatNaming:       flatNaming,
		AlwaysPullBase:   alwaysPullBase,
		BaseLang:         baseLang,
		Paths:            paths,
	}, nil
}
