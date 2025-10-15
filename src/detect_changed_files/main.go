package main

import (
	"errors"
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

// This program inspects git state to decide whether translation files changed.
// It supports both "flat" layouts (e.g., locales/en.json) and nested layouts
// (e.g., locales/en/app.json), and can optionally exclude base language files.
// Result is written as a GitHub Actions output variable `has_changes`.

// CommandRunner abstracts shell execution for testability (inject a fake runner).
type CommandRunner interface {
	Run(name string, args ...string) ([]string, error)
}

// DefaultCommandRunner executes commands via os/exec and returns non-empty stdout lines.
type DefaultCommandRunner struct{}

// Run executes the command and returns trimmed, non-empty lines of combined stdout/stderr.
// Rationale: git may emit warnings; we still want the file list from stdout.
// If exit code != 0 we bubble up the error with captured output for debugging CI logs.
func (d DefaultCommandRunner) Run(name string, args ...string) ([]string, error) {
	cmd := exec.Command(name, args...)
	outputBytes, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(outputBytes))

	if err != nil {
		return nil, fmt.Errorf("command '%s %s' failed: %v\nOutput:\n%s", name, strings.Join(args, " "), err, outputStr)
	}

	if outputStr == "" {
		return nil, nil // no changes / no matches
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

// Config aggregates inputs parsed from env.
type Config struct {
	FileExt        []string // normalized lowercased extensions without dots (e.g., "json", "strings")
	FlatNaming     bool     // true: locales/en.json; false: locales/en/*.json, locales/fr/*.json
	AlwaysPullBase bool     // if false, base language files/dirs are excluded from change detection
	BaseLang       string   // e.g., "en", "fr_FR"
	Paths          []string // one or more translation roots, e.g., ["locales"]
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

// detectChangedFiles collects modified + untracked files matching the given patterns,
// applies exclusion rules (base language, nested vs flat), and returns true if anything remains.
func detectChangedFiles(config *Config, runner CommandRunner) (bool, error) {
	// Modified/staged vs HEAD (or best-effort fallback if HEAD absent).
	statusFiles, err := gitDiff(config, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting changed files: %v", err)
	}

	// Untracked files (e.g., new language files created by the download).
	untrackedFiles, err := gitLsFiles(config, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting untracked files: %v", err)
	}

	// Merge and dedupe to avoid double-counting the same path.
	allChangedFiles := deduplicateFiles(statusFiles, untrackedFiles)

	// Precompute exclusion regexes based on layout and base language policy.
	excludePatterns, err := buildExcludePatterns(config)
	if err != nil {
		return false, fmt.Errorf("error building exclusion patterns: %v", err)
	}

	// Apply exclusions (e.g., ignore locales/en/* when AlwaysPullBase=false in nested mode).
	filteredFiles := filterFiles(allChangedFiles, excludePatterns)

	return len(filteredFiles) > 0, nil
}

// gitDiff runs `git diff --name-only HEAD -- <patterns>`.
// If HEAD is missing (e.g., initial commit/orphan), it falls back to combining
// staged (`--cached`) and unstaged diffs.
// Notes:
// - We pass explicit pathspecs to limit to translation files only.
// - We normalize slashes for cross-OS consistency.
func gitDiff(config *Config, runner CommandRunner) ([]string, error) {
	// Fast path when HEAD exists: changes relative to last commit (staged + unstaged).
	if _, err := runner.Run("git", "rev-parse", "--verify", "HEAD"); err == nil {
		args := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only", "HEAD")
		return runner.Run("git", args...)
	}

	// Fallback for repos without HEAD (rare in CI but can happen).
	var all []string

	// Staged changes (index vs HEAD).
	argsCached := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only", "--cached")
	if out, err := runner.Run("git", argsCached...); err == nil {
		all = append(all, out...)
	}

	// Unstaged changes (worktree vs index).
	argsWT := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "diff", "--name-only")
	if out, err := runner.Run("git", argsWT...); err == nil {
		all = append(all, out...)
	}

	// Deduplicate and normalize before returning.
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

// gitLsFiles runs `git ls-files --others --exclude-standard -- <patterns>`
// to get untracked files under the provided pathspecs.
func gitLsFiles(config *Config, runner CommandRunner) ([]string, error) {
	args := buildGitStatusArgs(config.Paths, config.FileExt, config.FlatNaming, "ls-files", "--others", "--exclude-standard")
	return runner.Run("git", args...)
}

// buildGitStatusArgs constructs the git command args:
// <git subcommand...> "--" <globbed pathspecs...>
// It builds per-path, per-extension globs and supports nested "**" for deep layouts.
// Example (flat):   locales/*.json
// Example (nested): locales/**/*.json
// We rely on git's pathspec globbing (not the shell), hence the explicit "--".
func buildGitStatusArgs(paths []string, fileExt []string, flatNaming bool, gitCmd ...string) []string {
	patterns := make([]string, 0, len(paths)*len(fileExt))

	for _, path := range paths {
		for _, ext := range fileExt {
			ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
			if ext == "" {
				continue
			}
			if flatNaming {
				patterns = append(patterns, filepath.ToSlash(filepath.Join(path, fmt.Sprintf("*.%s", ext))))
			} else {
				patterns = append(patterns, filepath.ToSlash(filepath.Join(path, "**", fmt.Sprintf("*.%s", ext))))
			}
		}
	}

	// git -c core.quotepath=false <subcmd...> -- <patterns...>
	args := append([]string{"-c", "core.quotepath=false"}, gitCmd...)
	args = append(args, "--")
	args = append(args, patterns...)
	return args
}

// deduplicateFiles merges two file lists and returns a sorted, de-duplicated slice.
// Normalizes path separators to forward slashes to avoid OS-dependent mismatches.
func deduplicateFiles(statusFiles, untrackedFiles []string) []string {
	fileSet := make(map[string]struct{})

	for _, file := range statusFiles {
		fileSet[filepath.ToSlash(strings.TrimSpace(file))] = struct{}{}
	}

	for _, file := range untrackedFiles {
		fileSet[filepath.ToSlash(strings.TrimSpace(file))] = struct{}{}
	}

	allFiles := make([]string, 0, len(fileSet))
	for file := range fileSet {
		allFiles = append(allFiles, file)
	}

	slices.Sort(allFiles) // keeps output deterministic for tests/logs

	return allFiles
}

// buildExcludePatterns returns a list of regexes representing files/dirs to ignore,
// based on naming mode and base language policy.
// Flat mode:
//   - If AlwaysPullBase=false, exclude "<path>/<base>.<ext>" for each ext.
//   - Always exclude subdirectories under <path> (flat layout shouldn't see nested dirs).
//
// Nested mode:
//   - If AlwaysPullBase=false, exclude "<path>/<base>/**".
func buildExcludePatterns(config *Config) ([]*regexp.Regexp, error) {
	excludePatterns := make([]*regexp.Regexp, 0, len(config.Paths)*(1+len(config.FileExt)))

	for _, path := range config.Paths {
		path = filepath.ToSlash(path)

		if config.FlatNaming {
			// Exclude base language single files per extension in flat layout.
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
			// In flat mode, suppress any nested directories to avoid accidental matches.
			patternStr := fmt.Sprintf("^%s/[^/]+/.*", regexp.QuoteMeta(path))
			pattern, err := regexp.Compile(patternStr)
			if err != nil {
				return nil, fmt.Errorf("failed to compile regex '%s': %v", patternStr, err)
			}

			excludePatterns = append(excludePatterns, pattern)
		} else {
			// Nested: exclude the entire base language subtree.
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

// filterFiles walks the given file list and drops those that match any exclusion regex.
// Paths are normalized to forward slashes before matching.
func filterFiles(files []string, excludePatterns []*regexp.Regexp) []string {
	if len(excludePatterns) == 0 {
		return files // nothing to exclude
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

// prepareConfig parses env vars, normalizes extensions, validates inputs.
// Behavior mirrors the action inputs: FILE_EXT may be multi-line, otherwise inferred from FILE_FORMAT.
func prepareConfig() (*Config, error) {
	flatNaming, err := parsers.ParseBoolEnv("FLAT_NAMING")
	if err != nil {
		return nil, fmt.Errorf("invalid FLAT_NAMING value: %v", err)
	}

	alwaysPullBase, err := parsers.ParseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return nil, fmt.Errorf("invalid ALWAYS_PULL_BASE value: %v", err)
	}

	rawPaths := parsers.ParseStringArrayEnv("TRANSLATIONS_PATH")
	if len(rawPaths) == 0 {
		return nil, fmt.Errorf("no valid paths found in TRANSLATIONS_PATH")
	}
	// validate + normalize repo-relative paths
	pathSet := make(map[string]struct{}, len(rawPaths))
	paths := make([]string, 0, len(rawPaths))
	for _, p := range rawPaths {
		clean, err := ensureRepoRelative(p)
		if err != nil {
			return nil, fmt.Errorf("invalid path %q in TRANSLATIONS_PATH: %w", p, err)
		}
		// normalize separators to forward slash for git pathspec
		norm := filepath.ToSlash(clean)
		if _, dup := pathSet[norm]; dup {
			continue
		}
		pathSet[norm] = struct{}{}
		paths = append(paths, norm)
	}

	fileExt := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExt) == 0 {
		if inferred := os.Getenv("FILE_FORMAT"); inferred != "" {
			fileExt = []string{inferred}
		}
	}
	if len(fileExt) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_FORMAT or FILE_EXT environment variables are set")
	}

	seen := make(map[string]struct{})
	norm := make([]string, 0, len(fileExt))
	for _, ext := range fileExt {
		e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if e == "" {
			continue
		}
		if strings.ContainsAny(e, `/\`) {
			return nil, fmt.Errorf("invalid file extension %q", ext)
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

	baseLang := strings.TrimSpace(os.Getenv("BASE_LANG"))
	if baseLang == "" {
		return nil, fmt.Errorf("BASE_LANG environment variable is required")
	}
	// keep baseLang as-is; we use it as path segment/file stem later

	return &Config{
		FileExt:        norm,
		FlatNaming:     flatNaming,
		AlwaysPullBase: alwaysPullBase,
		BaseLang:       baseLang,
		Paths:          paths,
	}, nil
}

// ensureRepoRelative validates that the path stays inside repo root and is relative.
// It rejects: absolute paths (including Windows drives and UNC), attempts to escape (".."),
// empty/whitespace, and normalizes cleaned relative path for further use.
func ensureRepoRelative(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty path")
	}

	clean := filepath.Clean(p)

	// Reject absolute (covers *nix, Windows drive letters and UNC)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative to repo: %q", p)
	}
	// Quick guard for UNC-like strings that filepath.IsAbs может не распознать в редких кейсах
	s := filepath.ToSlash(clean)
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return "", fmt.Errorf("path must be relative to repo: %q", p)
	}
	// Block going above repo
	if s == ".." || strings.HasPrefix(s, "../") {
		return "", fmt.Errorf("path escapes repo root: %q", p)
	}
	// Block Windows drive letters that slipped as relative (e.g. "C:foo")
	if matched, _ := regexp.MatchString(`^[A-Za-z]:`, s); matched {
		return "", fmt.Errorf("path must be relative (got drive-prefixed): %q", p)
	}
	return clean, nil
}
