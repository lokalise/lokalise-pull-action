package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
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
	FileExt        []string
	FlatNaming     bool
	AlwaysPullBase bool
	BaseLang       string
	Paths          []string
}

func main() {
	config, err := prepareConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error preparing configuration:", err)
		os.Exit(1)
	}

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
	// Check if HEAD exists
	if _, err := runner.Run("git", "rev-parse", "--verify", "HEAD"); err == nil {
		args := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only", "HEAD")
		return runner.Run("git", args...)
	}

	// HEAD missing. This is quite unexpected but might happen (initial commit / orphan). Combine staged and unstaged diffs.
	var all []string

	argsCached := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only", "--cached")
	if out, err := runner.Run("git", argsCached...); err == nil {
		all = append(all, out...)
	}

	// unstaged (worktree vs index)
	argsWT := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only")
	if out, err := runner.Run("git", argsWT...); err == nil {
		all = append(all, out...)
	}

	// dedup before returning
	seen := make(map[string]struct{}, len(all))
	out := make([]string, 0, len(all))
	for _, f := range all {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}

	return out, nil
}

// gitLsFiles runs `git ls-files --others --exclude-standard -- <patterns>` to detect untracked files.
func gitLsFiles(config *Config, runner CommandRunner) ([]string, error) {
	args := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "ls-files", "--others", "--exclude-standard")
	return runner.Run("git", args...)
}

// buildGitStatusArgs constructs git command arguments based on naming convention and paths.
// It builds glob patterns to match translation files for ALL provided extensions.
// Returns: <gitCmd...> "--" <patterns...>
func buildGitStatusArgs(paths []string, fileExt []string, flatNaming bool, gitCmd ...string) []string {
	patterns := make([]string, 0, len(paths)*len(fileExt))

	for _, path := range paths {
		for _, ext := range fileExt {
			ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
			if ext == "" {
				continue
			}
			if flatNaming {
				// For flat naming, match files like "path/*.ext"
				patterns = append(patterns, filepath.ToSlash(filepath.Join(path, fmt.Sprintf("*.%s", ext))))
			} else {
				// For nested directories, match files like "path/**/*.ext"
				patterns = append(patterns, filepath.ToSlash(filepath.Join(path, "**", fmt.Sprintf("*.%s", ext))))
			}
		}
	}

	args := append(gitCmd, "--")
	args = append(args, patterns...)
	return args
}

// deduplicateFiles combines and deduplicates two slices of files.
func deduplicateFiles(statusFiles, untrackedFiles []string) []string {
	fileSet := make(map[string]struct{})

	for _, file := range statusFiles {
		fileSet[filepath.ToSlash(strings.TrimSpace(file))] = struct{}{}
	}

	for _, file := range untrackedFiles {
		fileSet[filepath.ToSlash(strings.TrimSpace(file))] = struct{}{}
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
// Returns a slice of regular expression objects.
func buildExcludePatterns(config *Config) ([]*regexp.Regexp, error) {
	excludePatterns := make([]*regexp.Regexp, 0, len(config.Paths)*(1+len(config.FileExt)))

	for _, path := range config.Paths {
		path = filepath.ToSlash(path)

		if config.FlatNaming {
			// In flat naming we exclude baseLang file per extension if AlwaysPullBase=false
			if !config.AlwaysPullBase {
				for _, ext := range config.FileExt {
					ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
					if ext == "" {
						continue
					}
					baseLangFile := filepath.ToSlash(filepath.Join(path, fmt.Sprintf("%s.%s", config.BaseLang, ext)))
					patternStr := fmt.Sprintf("^%s$", regexp.QuoteMeta(baseLangFile))
					pattern, err := regexp.Compile(patternStr)
					if err != nil {
						return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
					}
					excludePatterns = append(excludePatterns, pattern)
				}
			}
			// Exclude subdirectories entirely in flat naming
			patternStr := fmt.Sprintf("^%s/[^/]+/.*", regexp.QuoteMeta(path))
			pattern, err := regexp.Compile(patternStr)
			if err != nil {
				return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
			}
			excludePatterns = append(excludePatterns, pattern)
		} else {
			// In nested naming we exclude the baseLang directory if AlwaysPullBase=false
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

func prepareConfig() (*Config, error) {
	// Parse boolean environment variables
	flatNaming, err := parsers.ParseBoolEnv("FLAT_NAMING")
	if err != nil {
		return nil, fmt.Errorf("invalid FLAT_NAMING value: %v", err)
	}

	alwaysPullBase, err := parsers.ParseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return nil, fmt.Errorf("invalid ALWAYS_PULL_BASE value: %v", err)
	}

	// Parse paths from TRANSLATIONS_PATH
	paths := parsers.ParseStringArrayEnv("TRANSLATIONS_PATH")
	if len(paths) == 0 {
		return nil, fmt.Errorf("no valid paths found in TRANSLATIONS_PATH")
	}

	// Parse FILE_EXT as newline-separated list; fall back to FILE_FORMAT if empty.
	fileExt := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExt) == 0 {
		if inferred := os.Getenv("FILE_FORMAT"); inferred != "" {
			fileExt = []string{inferred}
		}
	}
	if len(fileExt) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_FORMAT or FILE_EXT environment variables are set")
	}

	// Normalize and dedupe extensions
	seen := make(map[string]struct{})
	norm := make([]string, 0, len(fileExt))
	for _, ext := range fileExt {
		e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		norm = append(norm, e)
	}
	if len(norm) == 0 {
		return nil, fmt.Errorf("no valid file extensions after normalization")
	}

	baseLang := os.Getenv("BASE_LANG")
	if baseLang == "" {
		return nil, fmt.Errorf("BASE_LANG environment variable is required")
	}

	return &Config{
		FileExt:        norm,
		FlatNaming:     flatNaming,
		AlwaysPullBase: alwaysPullBase,
		BaseLang:       baseLang,
		Paths:          paths,
	}, nil
}
