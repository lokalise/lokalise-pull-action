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

func cmdKey(gitCmd []string, patterns []string) string {
	all := append(append([]string{}, gitCmd...), "--")
	all = append(all, patterns...)
	return filepath.ToSlash(strings.TrimSpace("git " + strings.Join(all, " ")))
}

func TestPrepareConfig(t *testing.T) {
	normalizeCfg := func(c *Config) *Config {
		if c == nil {
			return nil
		}
		cp := *c
		normPaths := make([]string, len(c.Paths))
		for i, p := range c.Paths {
			normPaths[i] = filepath.ToSlash(p)
		}
		cp.Paths = normPaths
		return &cp
	}

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
			expectedError: "no valid paths found in TRANSLATIONS_PATH",
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
			name: "Multiple TRANSLATIONS_PATH are cleaned and kept relative",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": ".\n./locales\nlocales/../locales",
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
				Paths:          []string{".", "locales", "locales"},
			},
		},
		{
			name: "Reject absolute TRANSLATIONS_PATH",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "/etc/locales",
				"FILE_EXT":          "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "invalid TRANSLATIONS_PATH",
		},
		{
			name: "Reject parent-escape TRANSLATIONS_PATH",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "../locales",
				"FILE_EXT":          "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "invalid TRANSLATIONS_PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// set envs
			for k, v := range tt.envVars {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("failed to set env: %v", err)
				}
			}
			// ensure required booleans have defaults if test omitted them
			if _, ok := tt.envVars["FLAT_NAMING"]; !ok {
				_ = os.Setenv("FLAT_NAMING", "true")
			}
			if _, ok := tt.envVars["ALWAYS_PULL_BASE"]; !ok {
				_ = os.Setenv("ALWAYS_PULL_BASE", "false")
			}

			defer func() {
				for k := range tt.envVars {
					_ = os.Unsetenv(k)
				}
				_ = os.Unsetenv("FLAT_NAMING")
				_ = os.Unsetenv("ALWAYS_PULL_BASE")
			}()

			cfg, err := prepareConfig()

			if tt.expectedError != "" {
				if err == nil || !containsSubstring(err.Error(), tt.expectedError) {
					t.Errorf("Expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			got := normalizeCfg(cfg)
			want := normalizeCfg(tt.expectedConfig)

			if !reflect.DeepEqual(got, want) {
				t.Errorf("Expected config %+v, got %+v", want, got)
			}
		})
	}
}

func TestBuildGitStatusArgs(t *testing.T) {
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
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "*.json")),
			},
		},
		{
			name:       "Nested naming (single ext, multiple paths)",
			paths:      []string{"path1", "path2"},
			fileExt:    []string{"json"},
			flatNaming: false,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "**", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "**", "*.json")),
			},
		},
		{
			name:       "Flat naming (multi-ext iOS bundle)",
			paths:      []string{"ios/LocA", "ios/LocB"},
			fileExt:    []string{"strings", "stringsdict"},
			flatNaming: true,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("ios/LocA", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/LocA", "*.stringsdict")),
				filepath.ToSlash(filepath.Join("ios/LocB", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/LocB", "*.stringsdict")),
			},
		},
		{
			name:       "Nested naming (multi-ext iOS bundle, mixed case + spaces + leading dot)",
			paths:      []string{"ios/App"},
			fileExt:    []string{".STRINGS", " stringsdict "},
			flatNaming: false,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("ios/App", "**", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/App", "**", "*.stringsdict")),
			},
		},
		{
			name:       "Empty extensions yields only git args and --",
			paths:      []string{"path1"},
			fileExt:    []string{},
			flatNaming: true,
			expected:   []string{"diff", "--name-only", "HEAD", "--"},
		},
		{
			name:       "Dedup paths/exts and trim ./ prefix",
			paths:      []string{"./locales", "locales"},
			fileExt:    []string{"JSON", ".json", " yaml "},
			flatNaming: true,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("locales", "*.json")),
				filepath.ToSlash(filepath.Join("locales", "*.yaml")),
			},
		},
		{
			name:       "Nested naming with dot root produces **/*.ext (no ./ prefix)",
			paths:      []string{"."},
			fileExt:    []string{"json"},
			flatNaming: false,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				"**/*.json",
			},
		},
		{
			name:       "Deterministic order with multiple paths and extensions",
			paths:      []string{"b", "a"},
			fileExt:    []string{"yaml", "json"},
			flatNaming: true,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("a", "*.json")),
				filepath.ToSlash(filepath.Join("a", "*.yaml")),
				filepath.ToSlash(filepath.Join("b", "*.json")),
				filepath.ToSlash(filepath.Join("b", "*.yaml")),
			},
		},
		{
			name:       "Ignores empty/whitespace extensions",
			paths:      []string{"path1"},
			fileExt:    []string{" ", "\t", "."},
			flatNaming: true,
			expected:   []string{"diff", "--name-only", "HEAD", "--"},
		},
		{
			name:       "Nested naming with mixed separators in input paths",
			paths:      []string{`ios\LocA`},
			fileExt:    []string{"strings", "stringsdict"},
			flatNaming: false,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("ios/LocA", "**", "*.strings")),
				filepath.ToSlash(filepath.Join("ios/LocA", "**", "*.stringsdict")),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitStatusArgs(tt.paths, tt.fileExt, tt.flatNaming, "diff", "--name-only", "HEAD")

			normalize := func(paths []string) []string {
				for i := range paths {
					paths[i] = filepath.ToSlash(paths[i])
				}
				return paths
			}
			got = normalize(got)
			expected := normalize(tt.expected)

			if !reflect.DeepEqual(got, expected) {
				t.Errorf("buildGitStatusArgs() = %v, want %v", got, expected)
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
		{
			name:  "Backslash paths are normalized before matching",
			files: []string{`loc\en.json`, `loc\fr.json`},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^loc/en\.json$`),
			},
			expected: []string{`loc/fr.json`},
		},
		{
			name:  "Leading ./ is normalized before matching",
			files: []string{"./loc/en.json", "loc/fr.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^loc/.*`),
			},
			expected: []string{}, // both excluded after normalization
		},
		{
			name:  "Regex metacharacters in filenames",
			files: []string{"loc/de+at.json", "loc/de.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^loc/de\+at\.json$`), // escape '+' so it matches literally
			},
			expected: []string{"loc/de.json"},
		},
		{
			name:            "Empty excludePatterns slice behaves like nil (no exclusions)",
			files:           []string{"a.json", "b.json"},
			excludePatterns: []*regexp.Regexp{}, // explicit empty slice
			expected:        []string{"a.json", "b.json"},
		},
		{
			name:  "Duplicates are preserved for non-excluded files",
			files: []string{"a.json", "a.json", "b.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^b\.json$`),
			},
			expected: []string{"a.json", "a.json"},
		},
		{
			name:  "Mixed slashes with directory-only exclude",
			files: []string{`base\one.json`, `base/two.json`, `other/three.json`},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile(`^base/.*`),
			},
			expected: []string{"other/three.json"},
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

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			cmdKey(diffArgs[:3], diffArgs[4:]): {
				filepath.ToSlash("path/to/translations/file1.json"),
				filepath.ToSlash("path/to/translations/file2.json"),
			},
			cmdKey(lsArgs[:3], lsArgs[4:]): {
				filepath.ToSlash("path/to/translations/file3.json"),
			},
		},
	}

	config := &Config{
		Paths:      paths,
		FileExt:    fileExts,
		FlatNaming: flat,
		// AlwaysPullBase defaults false is fine here; no baseLang excludes applied in detect step
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if !changed {
		t.Errorf("Expected changes, but got none")
	}
}

func TestDetectChangedFiles_NoChanges(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			cmdKey(diffArgs[:3], diffArgs[4:]): {},
			cmdKey(lsArgs[:3], lsArgs[4:]):     {},
		},
	}

	config := &Config{
		Paths:      paths,
		FileExt:    fileExts,
		FlatNaming: flat,
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if changed {
		t.Errorf("Expected no changes, but got changes")
	}
}

func TestDetectChangedFiles_AllChangesExcluded_Flat_PerExt(t *testing.T) {
	// flat naming: exclude baseLang file per extension if AlwaysPullBase=false
	paths := []string{"ios/Loc"}
	fileExts := []string{"strings", "stringsdict"}
	flat := true

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			cmdKey(diffArgs[:3], diffArgs[4:]): {
				filepath.ToSlash("ios/Loc/en.strings"),
				filepath.ToSlash("ios/Loc/en.stringsdict"),
			},
			cmdKey(lsArgs[:3], lsArgs[4:]): {},
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
		t.Errorf("Unexpected error: %v", err)
	}
	if changed {
		t.Errorf("Expected no changes (all changes excluded), but got changes")
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
			cmdKey(diffArgs[:3], diffArgs[4:]): {
				filepath.ToSlash("ios/App/en/Localizable.strings"),
				filepath.ToSlash("ios/App/en/Plurals.stringsdict"),
				filepath.ToSlash("ios/App/de/Localizable.strings"),
			},
			cmdKey(lsArgs[:3], lsArgs[4:]): {
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
			revParseKey: {}, // no output, but success
		},
		Err: map[string]error{
			// same as before: error on the actual diff HEAD call
			cmdKey(diffArgs[:3], diffArgs[4:]): fmt.Errorf("git diff error"),
		},
	}

	config := &Config{
		Paths:      paths,
		FileExt:    fileExts,
		FlatNaming: flat,
		// other fields not needed for this error path
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff error") {
		t.Errorf("Expected git diff error, but got %v", err)
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
			cmdKey(argsCached[:3], argsCached[4:]): {"locales/en.json"},
			cmdKey(argsWT[:3], argsWT[4:]):         {},
			cmdKey(argsLS[:3], argsLS[4:]):         {},
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
			cmdKey(argsCached[:3], argsCached[4:]): {},
			cmdKey(argsWT[:3], argsWT[4:]):         {},
			cmdKey(argsLS[:3], argsLS[4:]):         {},
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

func TestDetectChangedFiles_GitLsFilesError(t *testing.T) {
	paths := []string{"path/to/translations"}
	fileExts := []string{"json"}
	flat := true

	diffArgs := buildGitStatusArgs(paths, fileExts, flat, "diff", "--name-only", "HEAD")
	lsArgs := buildGitStatusArgs(paths, fileExts, flat, "ls-files", "--others", "--exclude-standard")

	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			cmdKey(diffArgs[:3], diffArgs[4:]): {},
		},
		Err: map[string]error{
			cmdKey(lsArgs[:3], lsArgs[4:]): fmt.Errorf("git ls-files error"),
		},
	}

	config := &Config{
		Paths:      paths,
		FileExt:    fileExts,
		FlatNaming: flat,
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git ls-files error") {
		t.Errorf("Expected git ls-files error, but got %v", err)
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
			expectedPatterns: []string{
				"^module/A/loc/en/.*",
				"^module/B/loc/en/.*",
			},
			expectError: false,
		},
		{
			name: "Flat naming, AlwaysPullBase = false, EMPTY FileExt → only subdir exclude",
			config: &Config{
				Paths:          []string{"flat"},
				FileExt:        []string{},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				"^flat/[^/]+/.*",
			},
			expectError: false,
		},
		{
			name: "Flat naming, paths with regex metachars are safely escaped",
			config: &Config{
				Paths:          []string{`module[1]+/loc.v2`},
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				`^module\[1\]\+/loc\.v2/en\.json$`,
				`^module\[1\]\+/loc\.v2/[^/]+/.*`,
			},
			expectError: false,
		},
		{
			name: "Flat naming, multiple paths & multi-ext — per-ext file excludes per path + subdir excludes",
			config: &Config{
				Paths:          []string{"pkg/a", "pkg/b"},
				FileExt:        []string{"json", "yaml"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				`^pkg/a/en\.json$`,
				`^pkg/a/en\.yaml$`,
				`^pkg/a/[^/]+/.*`,
				`^pkg/b/en\.json$`,
				`^pkg/b/en\.yaml$`,
				`^pkg/b/[^/]+/.*`,
			},
			expectError: false,
		},
		{
			name: "Nested naming, AlwaysPullBase = true → no excludes at all",
			config: &Config{
				Paths:          []string{"nested/loc"},
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: true,
				BaseLang:       "en",
			},
			expectedPatterns: []string{},
			expectError:      false,
		},
		{
			name: "Nested naming with Windows-like path gets normalized",
			config: &Config{
				Paths:          []string{`ios\Loc`},
				FileExt:        []string{"strings"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en-US",
			},
			expectedPatterns: []string{
				`^ios/Loc/en-US/.*`,
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

// containsSubstring checks if a string contains a substring.
func containsSubstring(str, substr string) bool {
	return strings.Contains(str, substr)
}
