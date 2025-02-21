package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type MockCommandRunner struct {
	RunFunc     func(name string, args ...string) error
	CaptureFunc func(name string, args ...string) (string, error)
}

func (m MockCommandRunner) Run(name string, args ...string) error {
	if m.RunFunc != nil {
		return m.RunFunc(name, args...)
	}
	return nil
}

func (m MockCommandRunner) Capture(name string, args ...string) (string, error) {
	if m.CaptureFunc != nil {
		return m.CaptureFunc(name, args...)
	}
	return "", nil
}

func TestEnvVarsToConfig(t *testing.T) {
	tests := []struct {
		name            string
		envVars         map[string]string
		expectedConfig  *Config
		expectError     bool
		expectedErrText string // Error substring to validate
	}{
		{
			name: "Valid environment variables",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"GITHUB_REF_NAME":    "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_FORMAT":        "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
			},
			expectedConfig: &Config{
				GitHubActor:      "test_actor",
				GitHubSHA:        "123456",
				GitHubRefName:    "main",
				TempBranchPrefix: "temp",
				FileExt:          "json",
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   false,
			},
			expectError: false,
		},
		{
			name: "FILE_EXT has precedence over FILE_FORMAT",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"GITHUB_REF_NAME":    "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_FORMAT":        "structured_json",
				"FILE_EXT":           "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
			},
			expectedConfig: &Config{
				GitHubActor:      "test_actor",
				GitHubSHA:        "123456",
				GitHubRefName:    "main",
				TempBranchPrefix: "temp",
				FileExt:          "json",
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   false,
			},
			expectError: false,
		},
		{
			name: "Missing required environment variable",
			envVars: map[string]string{
				"GITHUB_SHA":         "123456",
				"GITHUB_REF_NAME":    "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_FORMAT":        "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
			},
			expectError:     true,
			expectedErrText: "GITHUB_ACTOR",
		},
		{
			name: "Invalid boolean environment variable",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"GITHUB_REF_NAME":    "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_FORMAT":        "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "not_a_bool",
				"ALWAYS_PULL_BASE":   "true",
			},
			expectError:     true,
			expectedErrText: "FLAT_NAMING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Clear environment variables not in the test case
			allEnvVars := []string{
				"GITHUB_ACTOR", "GITHUB_SHA", "GITHUB_REF_NAME", "TEMP_BRANCH_PREFIX",
				"TRANSLATIONS_PATH", "FILE_FORMAT", "BASE_LANG", "FLAT_NAMING", "ALWAYS_PULL_BASE",
			}

			for _, key := range allEnvVars {
				if _, ok := tt.envVars[key]; !ok {
					os.Unsetenv(key)
				}
			}

			// Execute the function
			config, err := envVarsToConfig()

			// Check if an error was expected
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if !containsSubstring(err.Error(), tt.expectedErrText) {
					t.Errorf("Expected error containing '%s', but got '%v'", tt.expectedErrText, err)
				}
				return
			}

			// Ensure no error was returned
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Validate the resulting config
			if config == nil {
				t.Errorf("Expected config but got nil")
				return
			}
			if *config != *tt.expectedConfig {
				t.Errorf("Expected config %+v but got %+v", *tt.expectedConfig, *config)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxLength int
		expected  string
	}{
		{
			name:      "Valid characters",
			input:     "Valid_Characters-123",
			maxLength: 50,
			expected:  "Valid_Characters-123",
		},
		{
			name:      "Invalid characters",
			input:     "Invalid!@#Characters$$",
			maxLength: 50,
			expected:  "InvalidCharacters",
		},
		{
			name:      "Mixed characters",
			input:     "Valid_123!@#Invalid-456$$",
			maxLength: 50,
			expected:  "Valid_123Invalid-456",
		},
		{
			name:      "Exceeds maxLength",
			input:     "Valid_123_Invalid_456_Valid_789_Extra_Characters",
			maxLength: 20,
			expected:  "Valid_123_Invalid_45",
		},
		{
			name:      "Empty input",
			input:     "",
			maxLength: 10,
			expected:  "",
		},
		{
			name:      "Only invalid characters",
			input:     "!@#$%^&*()",
			maxLength: 10,
			expected:  "",
		},
		{
			name:      "Input equals maxLength",
			input:     "ValidInput123",
			maxLength: 13,
			expected:  "ValidInput123",
		},
		{
			name:      "Input shorter than maxLength",
			input:     "Short",
			maxLength: 10,
			expected:  "Short",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizeString(tt.input, tt.maxLength)
			if result != tt.expected {
				t.Errorf("sanitizeString(%q, %d) = %q; want %q", tt.input, tt.maxLength, result, tt.expected)
			}
		})
	}
}

func TestSetGitUser(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "test_actor" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "test_actor@users.noreply.github.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor: "test_actor",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCheckoutBranch(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "checkout" && args[1] == "-b" {
				if args[2] == "new_branch" {
					return nil // Simulate branch creation
				}
			}
			if name == "git" && args[0] == "checkout" {
				if args[1] == "existing_branch" {
					return nil // Simulate switching to existing branch
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	// Test branch creation
	err := checkoutBranch("new_branch", runner)
	if err != nil {
		t.Errorf("Unexpected error for new branch: %v", err)
	}

	// Test switching to existing branch
	err = checkoutBranch("existing_branch", runner)
	if err != nil {
		t.Errorf("Unexpected error for existing branch: %v", err)
	}
}

func TestCommitAndPush(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				return "nothing to commit, working tree clean", fmt.Errorf("nothing to commit")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" && args[1] == "origin" {
				return nil // Simulate successful push
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	err := commitAndPush("test_branch", runner)
	if err != ErrNoChanges {
		t.Errorf("Expected ErrNoChanges, got %v", err)
	}
}

func TestBuildGitAddArgs(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		mockPaths    []string
		expectedArgs []string
	}{
		{
			name: "Flat naming with AlwaysPullBase = true, single path",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: true,
			},
			mockPaths: []string{filepath.Join("path", "to", "translations")},
			expectedArgs: []string{
				filepath.Join("path", "to", "translations", "*.json"),
				":!" + filepath.Join("path", "to", "translations", "**", "*.json"),
			},
		},
		{
			name: "Flat naming with AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: true,
			},
			mockPaths: []string{
				filepath.Join("path1"),
				filepath.Join("path2"),
			},
			expectedArgs: []string{
				filepath.Join("path1", "*.json"),
				":!" + filepath.Join("path1", "**", "*.json"),
				filepath.Join("path2", "*.json"),
				":!" + filepath.Join("path2", "**", "*.json"),
			},
		},
		{
			name: "Flat naming with AlwaysPullBase = false, multiple paths",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: false,
			},
			mockPaths: []string{
				filepath.Join("path1"),
				filepath.Join("path2"),
			},
			expectedArgs: []string{
				filepath.Join("path1", "*.json"),
				":!" + filepath.Join("path1", "en.json"),
				":!" + filepath.Join("path1", "**", "*.json"),
				filepath.Join("path2", "*.json"),
				":!" + filepath.Join("path2", "en.json"),
				":!" + filepath.Join("path2", "**", "*.json"),
			},
		},
		{
			name: "Nested naming with AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     false,
				AlwaysPullBase: true,
			},
			mockPaths: []string{
				filepath.Join("path1"),
				filepath.Join("path2"),
			},
			expectedArgs: []string{
				filepath.Join("path1", "**", "*.json"),
				filepath.Join("path2", "**", "*.json"),
			},
		},
		{
			name: "Nested naming with AlwaysPullBase = false, multiple paths",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     false,
				AlwaysPullBase: false,
			},
			mockPaths: []string{
				filepath.Join("path1"),
				filepath.Join("path2"),
			},
			expectedArgs: []string{
				filepath.Join("path1", "**", "*.json"),
				":!" + filepath.Join("path1", "en", "**"),
				filepath.Join("path2", "**", "*.json"),
				":!" + filepath.Join("path2", "en", "**"),
			},
		},
		{
			name: "Empty translations path",
			config: &Config{
				FileExt:        "json",
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: true,
			},
			mockPaths:    []string{},
			expectedArgs: []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Set TRANSLATIONS_PATH environment variable
			// Join the mock paths with newline since ParseStringArrayEnv splits by newlines
			envValue := strings.Join(tt.mockPaths, "\n")
			if envValue != "" {
				os.Setenv("TRANSLATIONS_PATH", envValue)
				defer os.Unsetenv("TRANSLATIONS_PATH")
			} else {
				// Ensure env var is unset if empty
				os.Unsetenv("TRANSLATIONS_PATH")
			}

			got := buildGitAddArgs(tt.config)

			if !equalSlices(got, tt.expectedArgs) {
				t.Errorf("buildGitAddArgs() = %v, want %v", got, tt.expectedArgs)
			}
		})
	}
}

func TestGenerateBranchName(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		expectedError bool
		expectedStart string // Expected start of the branch name
	}{
		{
			name: "Valid inputs",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				GitHubRefName:    "feature_branch",
				TempBranchPrefix: "temp",
			},
			expectedError: false,
			expectedStart: "temp_feature_branch_123456_",
		},
		{
			name: "GITHUB_SHA too short",
			config: &Config{
				GitHubSHA:        "123",
				GitHubRefName:    "main",
				TempBranchPrefix: "temp",
			},
			expectedError: true,
		},
		{
			name: "GITHUB_REF_NAME with invalid characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				GitHubRefName:    "feature/branch!@#",
				TempBranchPrefix: "temp",
			},
			expectedError: false,
			expectedStart: "temp_featurebranch_abcdef_",
		},
		{
			name: "GITHUB_REF_NAME exceeding 50 characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				GitHubRefName:    strings.Repeat("a", 60),
				TempBranchPrefix: "temp",
			},
			expectedError: false,
			expectedStart: "temp_" + strings.Repeat("a", 50) + "_abcdef_",
		},
	}

	for _, tt := range tests {
		tt := tt // Capture variable

		t.Run(tt.name, func(t *testing.T) {
			branchName, err := generateBranchName(tt.config)
			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else {
					if !strings.HasPrefix(branchName, tt.expectedStart) {
						t.Errorf("Expected branch name to start with %q, but got %q", tt.expectedStart, branchName)
					}
				}
			}
		})
	}
}

func TestCommitAndPush_Success(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				return "Files committed", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" && args[1] == "origin" {
				return nil // Simulate successful push
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	err := commitAndPush("test_branch", runner)
	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}
}

func TestCommitAndPush_CommitError(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				return "", fmt.Errorf("commit failed")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			return nil
		},
	}

	err := commitAndPush("test_branch", runner)
	if err == nil {
		t.Errorf("Expected error, but got nil")
	} else if !strings.Contains(err.Error(), "failed to commit changes") {
		t.Errorf("Expected commit error, but got %v", err)
	}
}

func TestCommitAndPush_PushError(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				return "Files committed", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" && args[1] == "origin" {
				return fmt.Errorf("push failed")
			}
			return nil
		},
	}

	err := commitAndPush("test_branch", runner)
	if err == nil {
		t.Errorf("Expected error, but got nil")
	} else if !strings.Contains(err.Error(), "push failed") {
		t.Errorf("Expected push error, but got %v", err)
	}
}

// containsSubstring checks if a string contains a substring
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Sort both slices to normalize their order
	sortedA := append([]string{}, a...) // Create a copy to avoid modifying the original slices
	sortedB := append([]string{}, b...)
	slices.Sort(sortedA)
	slices.Sort(sortedB)

	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}
