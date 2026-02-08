package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

type MockCommandRunner struct {
	Output map[string][]string
	Err    map[string]error
}

func (m MockCommandRunner) Run(name string, args ...string) ([]string, error) {
	key := filepath.ToSlash(name + " " + strings.Join(args, " "))

	if err, ok := m.Err[key]; ok {
		return nil, err
	}
	if output, ok := m.Output[key]; ok {
		return output, nil
	}
	return nil, fmt.Errorf("command '%s' not mocked", key)
}

func makeKey(args []string) string {
	return filepath.ToSlash("git " + strings.Join(args, " "))
}

func TestPrepareConfig(t *testing.T) {
	absUnixLike, _ := filepath.Abs("some/abs/path")

	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  string
		expectedConfig *Config
	}{
		{
			name: "Valid config via FILE_FORMAT fallback",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT overrides FILE_FORMAT (single ext)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "structured_json",
				"FILE_EXT":          "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT multi-line (iOS bundle)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          "strings\nstringsdict",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "true",
			},
			expectedConfig: &Config{
				FileExt:        []string{"strings", "stringsdict"},
				FlatNaming:     false,
				AlwaysPullBase: true,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT normalization (leading dot, spaces, casing) and dedupe",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          ".JSON\n json \nJsOn",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "Missing TRANSLATIONS_PATH",
			envVars: map[string]string{
				"FILE_FORMAT":      "json",
				"BASE_LANG":        "en",
				"FLAT_NAMING":      "true",
				"ALWAYS_PULL_BASE": "false",
			},
			expectedError: "variable TRANSLATIONS_PATH is required",
		},
		{
			name: "Invalid FLAT_NAMING",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "invalid_bool",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "invalid FLAT_NAMING value",
		},
		{
			name: "No valid extensions after normalization",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          " \n . \n\t",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "no valid file extensions after normalization",
		},
		{
			name: "Normalizes relative paths and slashes",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "./a//b/../c",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "true",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: true,
				BaseLang:       "en",
				Paths:          []string{"a/c"},
			},
		},
		{
			name: "Rejects parent escape",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "../outside",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "escapes repo root",
		},
		{
			name: "Rejects absolute path (platform-agnostic)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": absUnixLike,
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "path must be relative",
		},
		{
			name: "Multiple paths are deduped and normalized",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "./x\nx/\n./x/",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"x"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k := range tt.envVars {
				t.Setenv(k, tt.envVars[k])
			}

			cfg, err := prepareConfig()

			if tt.expectedError != "" {
				if err == nil || !containsSubstring(err.Error(), tt.expectedError) {
					t.Fatalf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(cfg, tt.expectedConfig) {
				t.Fatalf("expected config %+v, got %+v", tt.expectedConfig, cfg)
			}
		})
	}
}

func TestBuildGitStatusArgs(t *testing.T) {
	t.Parallel()

	normalize := func(paths []string) []string {
		for i := range paths {
			paths[i] = filepath.ToSlash(paths[i])
		}
		return paths
	}

	tests := []struct {
		name       string
		paths      []string
		fileExt    []string
		flatNaming bool
		expected   []string
	}{
		{
			name:       "Flat naming (single ext, multiple paths)",
			paths:      []string{"path1", "path2"},
			fileExt:    []string{"json"},
			flatNaming: true,
			expected: normalize([]string{
				"-c", "core.quotepath=false",
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "*.json")),
			}),
		},
		{
			name:       "Nested naming (single ext, multiple paths)",
			paths:      []string{"path1", "path2"},
			fileExt:    []string{"json"},
			flatNaming: false,
			expected: normalize([]string{
				"-c", "core.quotepath=false",
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "**", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "**", "*.json")),
			}),
		},
		{
			name:       "Flat naming (multi-ext iOS bundle)",
			paths:      []string{"ios/LocA", "ios/LocB"},
			fileExt:    []string{"strings", "stringsdict"},
			flatNaming: true,
			expected: normalize([]string{
				"-c", "core.quotepath=false",
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("ios/LocA", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/LocA", "*.stringsdict")),
				filepath.ToSlash(filepath.Join("ios/LocB", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/LocB", "*.stringsdict")),
			}),
		},
		{
			name:       "Nested naming (multi-ext, mixed case + spaces + leading dot)",
			paths:      []string{"ios/App"},
			fileExt:    []string{".STRINGS", " stringsdict "}, // normalize to "strings", "stringsdict"
			flatNaming: false,
			expected: normalize([]string{
				"-c", "core.quotepath=false",
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("ios/App", "**", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/App", "**", "*.stringsdict")),
			}),
		},
		{
			name:       "Empty extensions yields only git args",
			paths:      []string{"path1"},
			fileExt:    []string{},
			flatNaming: true,
			expected: []string{
				"-c", "core.quotepath=false",
				"diff", "--name-only", "HEAD",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitStatusArgs(tt.paths, tt.fileExt, tt.flatNaming, "diff", "--name-only", "HEAD")
			got = normalize(got)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("buildGitStatusArgs() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDeduplicateFiles(t *testing.T) {
	tests := []struct {
		name           string
		statusFiles    []string
		untrackedFiles []string
		expected       []string
	}{
		{
			name:           "No duplicates",
			statusFiles:    []string{"file1.json", "file2.json"},
			untrackedFiles: []string{"file3.json"},
			expected:       []string{"file1.json", "file2.json", "file3.json"},
		},
		{
			name:           "With duplicates",
			statusFiles:    []string{"file1.json", "file2.json"},
			untrackedFiles: []string{"file2.json", "file3.json"},
			expected:       []string{"file1.json", "file2.json", "file3.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deduplicateFiles(tt.statusFiles, tt.untrackedFiles)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("deduplicateFiles() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFilterFiles(t *testing.T) {
	tests := []struct {
		name            string
		files           []string
		excludePatterns []*regexp.Regexp
		expected        []string
	}{
		{
			name:            "No exclusions",
			files:           []string{"file1.json", "file2.json"},
			excludePatterns: nil,
			expected:        []string{"file1.json", "file2.json"},
		},
		{
			name:  "With exclusions (subdir prefix)",
			files: []string{"file1.json", "file2.json", "base/file3.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile("^base/.*"),
			},
			expected: []string{"file1.json", "file2.json"},
		},
		{
			name: "Multiple exclude patterns (exact file + directory)",
			files: []string{
				"loc/en.json",
				"loc/en/extra.json",
				"loc/de.json",
				"loc/fr/strings.json",
			},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^loc/en\.json$`), // exact
				regexp.MustCompile(`^loc/en/.*`),     // dir
			},
			expected: []string{
				"loc/de.json",
				"loc/fr/strings.json",
			},
		},
		{
			name:            "Empty files list",
			files:           []string{},
			excludePatterns: []*regexp.Regexp{regexp.MustCompile(`^whatever/.*`)},
			expected:        []string{},
		},
		{
			name:  "Exclude everything",
			files: []string{"a.json", "b.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^.*$`),
			},
			expected: []string{},
		},
		{
			name:  "Order is preserved for non-excluded files",
			files: []string{"1.json", "kill.json", "2.json", "keep/3.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^kill\.json$`),
			},
			expected: []string{"1.json", "2.json", "keep/3.json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterFiles(tt.files, tt.excludePatterns)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("filterFiles() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetectChangedFiles(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			// Make HEAD "exist" so gitDiff uses the HEAD-based diff path.
			revParseKey: {"ok"},

			makeKey(diffArgs): {
				filepath.ToSlash("path/to/translations/file1.json"),
				filepath.ToSlash("path/to/translations/file2.json"),
			},
			makeKey(lsArgs): {
				filepath.ToSlash("path/to/translations/file3.json"),
			},
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected changes, but got none")
	}
}

func TestDetectChangedFiles_NoChanges(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			// Make HEAD "exist" so we're testing the main path.
			revParseKey: {"ok"},

			makeKey(diffArgs): {},
			makeKey(lsArgs):   {},
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		// BaseLang isn't needed here because AlwaysPullBase=true (no exclusion),
		// but leaving it out is fine.
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected no changes, but got changes")
	}
}

func TestDetectChangedFiles_AllChangesExcluded_Flat_PerExt(t *testing.T) {
	paths := []string{"ios/Loc"}
	fileExts := []string{"strings", "stringsdict"}
	flat := true

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			makeKey([]string{"rev-parse", "--verify", "HEAD"}): {"ok"},

			makeKey(diffArgs): {
				filepath.ToSlash("ios/Loc/en.strings"),
				filepath.ToSlash("ios/Loc/en.stringsdict"),
			},
			makeKey(lsArgs): {},
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("Expected no changes (all changes excluded), but got changes")
	}
}

func TestDetectChangedFiles_Nested_BaseDirExcluded(t *testing.T) {
	paths := []string{"ios/App"}
	fileExts := []string{"strings", "stringsdict"}
	flat := false

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			revParseKey: {},
			makeKey(diffArgs): {
				filepath.ToSlash("ios/App/en/Localizable.strings"),
				filepath.ToSlash("ios/App/en/Plurals.stringsdict"),
				filepath.ToSlash("ios/App/de/Localizable.strings"),
			},
			makeKey(lsArgs): {
				filepath.ToSlash("ios/App/en/Untracked.strings"),
			},
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: false,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("Expected changes (non-base dir files remain), but got none")
	}
}

func TestDetectChangedFiles_GitDiffError(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")
	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			revParseKey: {},
		},
		Err: map[string]error{
			makeKey(diffArgs): fmt.Errorf("git diff error"),
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff error") {
		t.Fatalf("Expected git diff error, but got %v", err)
	}
}

func TestDetectChangedFiles_GitLsFilesError(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")
	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			revParseKey:       {},
			makeKey(diffArgs): {},
		},
		Err: map[string]error{
			makeKey(lsArgs): fmt.Errorf("git ls-files error"),
		},
	}

	config := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git ls-files error") {
		t.Fatalf("Expected git ls-files error, but got %v", err)
	}
}

func TestGitDiff_NoHead_Fallbacks(t *testing.T) {
	paths := []string{"locales"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")

	argsCached := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "--cached")
	argsWT := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only")
	argsLS := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mock := MockCommandRunner{
		Err: map[string]error{
			revParseKey: fmt.Errorf("bad revision 'HEAD'"),
		},
		Output: map[string][]string{
			makeKey(argsCached): {filepath.ToSlash("locales/en.json")},
			makeKey(argsWT):     {},
			makeKey(argsLS):     {},
		},
	}

	cfg := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected changes to be detected")
	}
}

func TestGitDiff_NoHead_NoChanges(t *testing.T) {
	paths := []string{"locales"}
	fileExts := []string{"json"}
	flat := true

	revParseKey := filepath.ToSlash("git rev-parse --verify HEAD")
	argsCached := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "--cached")
	argsWT := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only")
	argsLS := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mock := MockCommandRunner{
		Err: map[string]error{
			revParseKey: fmt.Errorf("bad revision 'HEAD'"),
		},
		Output: map[string][]string{
			makeKey(argsCached): {},
			makeKey(argsWT):     {},
			makeKey(argsLS):     {},
		},
	}

	cfg := &Config{
		Paths:          paths,
		FileExt:        fileExts,
		FlatNaming:     flat,
		AlwaysPullBase: true,
		BaseLang:       "en",
	}

	changed, err := detectChangedFiles(cfg, mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatalf("expected no changes")
	}
}

func TestBuildExcludePatterns(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		expectedPatterns []string
		expectError      bool
	}{
		{
			name: "Flat naming, AlwaysPullBase = false (single ext)",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				"^path/to/translations/en\\.json$",
				"^path/to/translations/[^/]+/.*",
			},
			expectError: false,
		},
		{
			name: "Nested naming, AlwaysPullBase = false (single ext)",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				"^path/to/translations/en/.*",
			},
			expectError: false,
		},
		{
			name: "Flat naming, AlwaysPullBase = true (single ext)",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: true,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				"^path/to/translations/[^/]+/.*",
			},
			expectError: false,
		},
		{
			name: "Flat naming, AlwaysPullBase = false (multi-ext iOS)",
			config: &Config{
				Paths:          []string{"ios/Loc"},
				FileExt:        []string{"strings", "stringsdict"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			// per-ext base file excludes, then subdir exclude
			expectedPatterns: []string{
				"^ios/Loc/en\\.strings$",
				"^ios/Loc/en\\.stringsdict$",
				"^ios/Loc/[^/]+/.*",
			},
			expectError: false,
		},
		{
			name: "Nested naming, AlwaysPullBase = false (multi-ext iOS) — only base dir excluded once",
			config: &Config{
				Paths:          []string{"ios/App"},
				FileExt:        []string{"strings", "stringsdict"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			// exclude base dir (not per-ext)
			expectedPatterns: []string{
				"^ios/App/en/.*",
			},
			expectError: false,
		},
		{
			name: "Nested naming, two paths, AlwaysPullBase = false",
			config: &Config{
				Paths:          []string{"module/A/loc", "module/B/loc"},
				FileExt:        []string{"yml"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			// one exclude per path
			expectedPatterns: []string{
				"^module/A/loc/en/.*",
				"^module/B/loc/en/.*",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			normalizePatterns := func(patterns []string) []string {
				var normalized []string
				for _, p := range patterns {
					normalized = append(normalized, filepath.ToSlash(p))
				}
				return normalized
			}
			normalizedExpectedPatterns := normalizePatterns(tt.expectedPatterns)

			patterns, err := buildExcludePatterns(tt.config)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			var patternStrings []string
			for _, p := range patterns {
				patternStrings = append(patternStrings, p.String())
			}

			normalizedPatternStrings := normalizePatterns(patternStrings)

			if !reflect.DeepEqual(normalizedPatternStrings, normalizedExpectedPatterns) {
				t.Errorf("Expected patterns %v, got %v", normalizedExpectedPatterns, normalizedPatternStrings)
			}
		})
	}
}

func TestHelperProcess_CommandRunner(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	write := func(f *os.File, s string) {
		if _, err := f.WriteString(s); err != nil {
			// hard exit: we're in helper process anyway
			os.Exit(3)
		}
	}

	switch os.Getenv("HELPER_MODE") {
	case "ok":
		write(os.Stdout, "path/to/translations/a.json\n")
		write(os.Stderr, "warning: not a file\n")
		os.Exit(0)
	case "fail":
		write(os.Stdout, "path/to/translations/b.json\n")
		write(os.Stderr, "boom\n")
		os.Exit(2)
	default:
		os.Exit(0)
	}
}

func TestDefaultCommandRunner_IgnoresStderrOnSuccess(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_MODE", "ok")

	r := DefaultCommandRunner{}
	out, err := r.Run(os.Args[0], "-test.run=TestHelperProcess_CommandRunner")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	want := []string{"path/to/translations/a.json"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("out mismatch. got=%v want=%v", out, want)
	}
}

func TestDefaultCommandRunner_IncludesStderrInError(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv("HELPER_MODE", "fail")

	r := DefaultCommandRunner{}
	_, err := r.Run(os.Args[0], "-test.run=TestHelperProcess_CommandRunner")
	if err == nil {
		t.Fatalf("expected err")
	}
	if !strings.Contains(err.Error(), "Stderr:") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected stderr in error, got: %v", err)
	}
}

// containsSubstring checks if a string contains a substring.
func containsSubstring(str, substr string) bool {
	return strings.Contains(str, substr)
}
