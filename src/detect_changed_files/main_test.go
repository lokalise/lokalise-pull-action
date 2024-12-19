package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
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

func TestPrepareConfig(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  string
		expectedConfig *Config
	}{
		{
			name: "Valid config",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileFormat:     "json",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					os.Unsetenv(key)
				}
			}()

			// Call the function
			config, err := prepareConfig()

			// Verify the result
			if tt.expectedError != "" {
				if err == nil || !containsSubstring(err.Error(), tt.expectedError) {
					t.Errorf("Expected error '%s', got '%v'", tt.expectedError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if !reflect.DeepEqual(config, tt.expectedConfig) {
					t.Errorf("Expected config %+v, got %+v", tt.expectedConfig, config)
				}
			}
		})
	}
}

func TestBuildGitStatusArgs(t *testing.T) {
	tests := []struct {
		name       string
		paths      []string
		fileFormat string
		flatNaming bool
		expected   []string
	}{
		{
			name:       "Flat naming",
			paths:      []string{"path1", "path2"},
			fileFormat: "json",
			flatNaming: true,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "*.json")),
			},
		},
		{
			name:       "Nested naming",
			paths:      []string{"path1", "path2"},
			fileFormat: "json",
			flatNaming: false,
			expected: []string{
				"diff", "--name-only", "HEAD", "--",
				filepath.ToSlash(filepath.Join("path1", "**", "*.json")),
				filepath.ToSlash(filepath.Join("path2", "**", "*.json")),
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitStatusArgs(tt.paths, tt.fileFormat, tt.flatNaming, "diff", "--name-only", "HEAD")

			// Normalize `got` and `expected` paths for comparison
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
		tt := tt
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
			name:  "With exclusions",
			files: []string{"file1.json", "file2.json", "base/file3.json"},
			excludePatterns: []*regexp.Regexp{
				regexp.MustCompile("^base/.*"),
			},
			expected: []string{"file1.json", "file2.json"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterFiles(tt.files, tt.excludePatterns)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("filterFiles() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetectChangedFiles(t *testing.T) {
	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			filepath.ToSlash("git diff --name-only HEAD -- path/to/translations/*.json"): {
				filepath.ToSlash("path/to/translations/file1.json"),
				filepath.ToSlash("path/to/translations/file2.json"),
			},
			filepath.ToSlash("git ls-files --others --exclude-standard -- path/to/translations/*.json"): {
				filepath.ToSlash("path/to/translations/file3.json"),
			},
		},
	}

	config := &Config{
		Paths:      []string{"path/to/translations"},
		FileFormat: "json",
		FlatNaming: true,
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
	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			filepath.ToSlash("git diff --name-only HEAD -- path/to/translations/*.json"):                {},
			filepath.ToSlash("git ls-files --others --exclude-standard -- path/to/translations/*.json"): {},
		},
	}

	config := &Config{
		Paths:      []string{"path/to/translations"},
		FileFormat: "json",
		FlatNaming: true,
	}

	changed, err := detectChangedFiles(config, mockRunner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if changed {
		t.Errorf("Expected no changes, but got changes")
	}
}

func TestDetectChangedFiles_AllChangesExcluded(t *testing.T) {
	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			filepath.ToSlash("git diff --name-only HEAD -- path/to/translations/*.json"): {
				filepath.ToSlash("path/to/translations/en.json"), // BaseLang file
			},
			filepath.ToSlash("git ls-files --others --exclude-standard -- path/to/translations/*.json"): {},
		},
	}

	config := &Config{
		Paths:          []string{"path/to/translations"},
		FileFormat:     "json",
		FlatNaming:     true,
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

func TestDetectChangedFiles_GitDiffError(t *testing.T) {
	mockRunner := MockCommandRunner{
		Err: map[string]error{
			filepath.ToSlash("git diff --name-only HEAD -- path/to/translations/*.json"): fmt.Errorf("git diff error"),
		},
	}

	config := &Config{
		Paths:      []string{"path/to/translations"},
		FileFormat: "json",
		FlatNaming: true,
	}

	_, err := detectChangedFiles(config, mockRunner)
	if err == nil || !strings.Contains(err.Error(), "git diff error") {
		t.Errorf("Expected git diff error, but got %v", err)
	}
}

func TestDetectChangedFiles_GitLsFilesError(t *testing.T) {
	mockRunner := MockCommandRunner{
		Output: map[string][]string{
			filepath.ToSlash("git diff --name-only HEAD -- path/to/translations/*.json"): {},
		},
		Err: map[string]error{
			filepath.ToSlash("git ls-files --others --exclude-standard -- path/to/translations/*.json"): fmt.Errorf("git ls-files error"),
		},
	}

	config := &Config{
		Paths:      []string{"path/to/translations"},
		FileFormat: "json",
		FlatNaming: true,
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
			name: "Flat naming, AlwaysPullBase = false",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileFormat:     "json",
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
			name: "Nested naming, AlwaysPullBase = false",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileFormat:     "json",
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
			name: "Flat naming, AlwaysPullBase = true",
			config: &Config{
				Paths:          []string{"path/to/translations"},
				FileFormat:     "json",
				FlatNaming:     true,
				AlwaysPullBase: true,
				BaseLang:       "en",
			},
			expectedPatterns: []string{
				"^path/to/translations/[^/]+/.*",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Normalize expected patterns to use forward slashes
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
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else {
					// Convert the compiled regex patterns back to strings to compare
					var patternStrings []string
					for _, p := range patterns {
						patternStrings = append(patternStrings, p.String())
					}
					// Normalize actual patterns
					normalizedPatternStrings := normalizePatterns(patternStrings)

					if !reflect.DeepEqual(normalizedPatternStrings, normalizedExpectedPatterns) {
						t.Errorf("Expected patterns %v, got %v", normalizedExpectedPatterns, normalizedPatternStrings)
					}
				}
			}
		})
	}
}

func TestParseBoolEnv(t *testing.T) {
	tests := []struct {
		name        string
		envVar      string
		envValue    string
		expected    bool
		expectError bool
	}{
		{
			name:        "Variable not set",
			envVar:      "TEST_BOOL_ENV",
			envValue:    "",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Variable set to true",
			envVar:      "TEST_BOOL_ENV",
			envValue:    "true",
			expected:    true,
			expectError: false,
		},
		{
			name:        "Variable set to false",
			envVar:      "TEST_BOOL_ENV",
			envValue:    "false",
			expected:    false,
			expectError: false,
		},
		{
			name:        "Variable set to invalid value",
			envVar:      "TEST_BOOL_ENV",
			envValue:    "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.envVar, tt.envValue)
			} else {
				os.Unsetenv(tt.envVar)
			}
			defer os.Unsetenv(tt.envVar)

			result, err := parsers.ParseBoolEnv(tt.envVar)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if result != tt.expected {
					t.Errorf("Expected %v but got %v", tt.expected, result)
				}
			}
		})
	}
}

// containsSubstring checks if a string contains a substring.
func containsSubstring(str, substr string) bool {
	return strings.Contains(str, substr)
}
