package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		expectedErrText string
	}{
		{
			name: "Valid environment variables",
			envVars: map[string]string{
				"BASE_REF":             "main",
				"GITHUB_ACTOR":         "test_actor",
				"GITHUB_SHA":           "123456",
				"TEMP_BRANCH_PREFIX":   "temp",
				"TRANSLATIONS_PATH":    "translations/",
				"FILE_FORMAT":          "json",
				"BASE_LANG":            "en",
				"FLAT_NAMING":          "true",
				"ALWAYS_PULL_BASE":     "false",
				"GIT_USER_NAME":        "my_user",
				"GIT_USER_EMAIL":       "test@example.com",
				"OVERRIDE_BRANCH_NAME": "custom_branch",
				"GIT_COMMIT_MESSAGE":   "My commit msg",
			},
			expectedConfig: &Config{
				GitHubActor:        "test_actor",
				GitHubSHA:          "123456",
				BaseRef:            "main",
				TempBranchPrefix:   "temp",
				FileExt:            []string{"json"},
				BaseLang:           "en",
				FlatNaming:         true,
				AlwaysPullBase:     false,
				GitUserName:        "my_user",
				GitUserEmail:       "test@example.com",
				OverrideBranchName: "custom_branch",
				GitCommitMessage:   "My commit msg",
			},
			expectError: false,
		},
		{
			name: "FILE_EXT normalization and dedupe",
			envVars: map[string]string{
				"BASE_REF":           "main",
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_EXT":           "\n .JSON \n  yaml  \n .json \n", // messy input
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "false",
				"ALWAYS_PULL_BASE":   "true",
			},
			expectedConfig: &Config{
				GitHubActor:      "test_actor",
				GitHubSHA:        "123456",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
				FileExt:          []string{"json", "yaml"}, // normalized, deduped, lowercased
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   true,
				GitCommitMessage: "Translations update",
			},
			expectError: false,
		},
		{
			name: "Empty commit msg",
			envVars: map[string]string{
				"GITHUB_ACTOR":         "test_actor",
				"GITHUB_SHA":           "123456",
				"BASE_REF":             "main",
				"TEMP_BRANCH_PREFIX":   "temp",
				"TRANSLATIONS_PATH":    "translations/",
				"FILE_FORMAT":          "json",
				"BASE_LANG":            "en",
				"FLAT_NAMING":          "true",
				"ALWAYS_PULL_BASE":     "false",
				"GIT_USER_NAME":        "my_user",
				"GIT_USER_EMAIL":       "test@example.com",
				"OVERRIDE_BRANCH_NAME": "custom_branch",
			},
			expectedConfig: &Config{
				GitHubActor:        "test_actor",
				GitHubSHA:          "123456",
				BaseRef:            "main",
				TempBranchPrefix:   "temp",
				FileExt:            []string{"json"},
				BaseLang:           "en",
				FlatNaming:         true,
				AlwaysPullBase:     false,
				GitUserName:        "my_user",
				GitUserEmail:       "test@example.com",
				OverrideBranchName: "custom_branch",
				GitCommitMessage:   "Translations update",
			},
			expectError: false,
		},
		{
			name: "FILE_EXT has precedence over FILE_FORMAT",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"BASE_REF":           "main",
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
				BaseRef:          "main",
				TempBranchPrefix: "temp",
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   false,
				GitCommitMessage: "Translations update",
			},
			expectError: false,
		},
		{
			name: "Missing required environment variable",
			envVars: map[string]string{
				"GITHUB_SHA":         "123456",
				"BASE_REF":           "main",
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
				"BASE_REF":           "main",
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
		{
			name: "FILE_EXT multiple values",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"BASE_REF":           "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations/",
				"FILE_EXT":           "strings\nstringsdict",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "false",
				"ALWAYS_PULL_BASE":   "true",
			},
			expectedConfig: &Config{
				GitHubActor:      "test_actor",
				GitHubSHA:        "123456",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
				FileExt:          []string{"strings", "stringsdict"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   true,
				GitCommitMessage: "Translations update",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// scrub env so CI-provided vars don't bleed into tests
			allEnvVars := []string{
				"GITHUB_ACTOR",
				"GITHUB_SHA",
				"BASE_REF",
				"TEMP_BRANCH_PREFIX",
				"TRANSLATIONS_PATH",
				"BASE_LANG",
				"FLAT_NAMING",
				"ALWAYS_PULL_BASE",
				"FORCE_PUSH",
				"GIT_USER_NAME",
				"GIT_USER_EMAIL",
				"OVERRIDE_BRANCH_NAME",
				"GIT_COMMIT_MESSAGE",
				"FILE_FORMAT",
				"FILE_EXT",
			}
			for _, k := range allEnvVars {
				t.Setenv(k, "") // treat as missing
			}

			// now set only what this test needs
			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			// Execute
			config, err := envVarsToConfig()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				} else if !containsSubstring(err.Error(), tt.expectedErrText) {
					t.Errorf("Expected error containing '%s', but got '%v'", tt.expectedErrText, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if config == nil {
				t.Errorf("Expected config but got nil")
				return
			}
			if !reflect.DeepEqual(config, tt.expectedConfig) {
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
		{
			name:      "Allow branch folders",
			input:     "feature/valid-branch-name",
			maxLength: 50,
			expected:  "feature/valid-branch-name",
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

func TestSetGitUser_WithCustomValues(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "custom_user" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "custom_email@example.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor:  "ignored_actor",
		GitUserName:  "custom_user",
		GitUserEmail: "custom_email@example.com",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestCheckoutBranch(t *testing.T) {
	t.Run("creates from origin base", func(t *testing.T) {
		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				switch {
				// first attempt should be: git checkout -B new_branch origin/main
				case len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "new_branch" && args[3] == "origin/main":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}
		// headRef is empty -> create from base
		if err := checkoutBranch("new_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("falls back to local base", func(t *testing.T) {
		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				switch {
				// first attempt (remote) fails
				case len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "branch_from_local" && args[3] == "origin/dev":
					return fmt.Errorf("remote branch missing")
				// second attempt should be local base: git checkout -B branch_from_local dev
				case len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "branch_from_local" && args[3] == "dev":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}
		// headRef is empty -> fallback path uses local base
		if err := checkoutBranch("branch_from_local", "dev", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("switches to existing branch", func(t *testing.T) {
		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				switch {
				// fail remote create
				case len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "existing_branch" && args[3] == "origin/main":
					return fmt.Errorf("already exists")
				// fail local create
				case len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "existing_branch" && args[3] == "main":
					return fmt.Errorf("already exists")
				// succeed switching
				case len(args) == 2 && args[0] == "checkout" && args[1] == "existing_branch":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}
		// headRef empty -> fall back to switching to existing branch
		if err := checkoutBranch("existing_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("updates existing PR head branch (uses headRef)", func(t *testing.T) {
		runner := &MockCommandRunner{
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				// expect: checkout -B new_br origin/new_br
				if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "new_br" && args[3] == "origin/new_br" {
					return nil
				}
				return fmt.Errorf("unexpected command: git %v", args)
			},
			CaptureFunc: func(name string, args ...string) (string, error) {
				// allow fetch noise if you call it in your code before checkout
				return "", nil
			},
		}
		// headRef equals branchName -> base off origin/headRef, not baseRef
		if err := checkoutBranch("new_br", "main", "new_br", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCommitAndPush(t *testing.T) {
	expectedMessage := "my test commit message"

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				// Check that -m flag is present
				foundM := false
				foundMsg := false
				for i := 0; i < len(args); i++ {
					if args[i] == "-m" {
						foundM = true
						if i+1 < len(args) && args[i+1] == expectedMessage {
							foundMsg = true
						}
					}
				}
				if !foundM || !foundMsg {
					t.Errorf("Expected -m %q in git commit args, got: %v", expectedMessage, args)
				}

				return "nothing to commit, working tree clean", fmt.Errorf("nothing to commit")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" && args[1] == "origin" {
				return nil
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitCommitMessage: expectedMessage,
	}

	err := commitAndPush("test_branch", runner, config)
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
				FileExt:        []string{"json"},
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
				FileExt:        []string{"json"},
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
				FileExt:        []string{"json"},
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
				FileExt:        []string{"json"},
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
				FileExt:        []string{"json"},
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
				FileExt:        []string{"json"},
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: true,
			},
			mockPaths:    []string{},
			expectedArgs: []string{},
		},
		{
			name: "Flat naming + multi-ext (iOS) + AlwaysPullBase = false, single path",
			config: &Config{
				FileExt:        []string{"strings", "stringsdict"},
				BaseLang:       "en",
				FlatNaming:     true,
				AlwaysPullBase: false,
			},
			mockPaths: []string{filepath.Join("ios", "Localizations")},
			expectedArgs: []string{
				// strings
				filepath.Join("ios", "Localizations", "*.strings"),
				":!" + filepath.Join("ios", "Localizations", "en.strings"),
				":!" + filepath.Join("ios", "Localizations", "**", "*.strings"),
				// stringsdict
				filepath.Join("ios", "Localizations", "*.stringsdict"),
				":!" + filepath.Join("ios", "Localizations", "en.stringsdict"),
				":!" + filepath.Join("ios", "Localizations", "**", "*.stringsdict"),
			},
		},
		{
			name: "Nested naming + multi-ext (iOS) + AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:        []string{"strings", "stringsdict"},
				BaseLang:       "en",
				FlatNaming:     false,
				AlwaysPullBase: true,
			},
			mockPaths: []string{
				filepath.Join("ios", "ModuleA"),
				filepath.Join("ios", "ModuleB"),
			},
			expectedArgs: []string{
				// ModuleA both exts
				filepath.Join("ios", "ModuleA", "**", "*.strings"),
				filepath.Join("ios", "ModuleA", "**", "*.stringsdict"),
				// ModuleB both exts
				filepath.Join("ios", "ModuleB", "**", "*.strings"),
				filepath.Join("ios", "ModuleB", "**", "*.stringsdict"),
			},
		},
		{
			name: "Nested naming + multi-ext (iOS) + AlwaysPullBase = false (exclude base dir once per path)",
			config: &Config{
				FileExt:        []string{"strings", "stringsdict"},
				BaseLang:       "en",
				FlatNaming:     false,
				AlwaysPullBase: false,
			},
			mockPaths: []string{
				filepath.Join("ios", "App"),
			},
			expectedArgs: []string{
				filepath.Join("ios", "App", "**", "*.strings"),
				filepath.Join("ios", "App", "**", "*.stringsdict"),
				":!" + filepath.Join("ios", "App", "en", "**"),
			},
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
				BaseRef:          "feature_branch",
				TempBranchPrefix: "temp",
			},
			expectedError: false,
			expectedStart: "temp_feature_branch_123456_",
		},
		{
			name: "Valid inputs with branch override",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "custom_branch",
			},
			expectedError: false,
			expectedStart: "custom_branch",
		},
		{
			name: "GITHUB_SHA too short",
			config: &Config{
				GitHubSHA:        "123",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
			},
			expectedError: true,
		},
		{
			name: "BASE_REF with invalid characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "feature/branch!@#",
				TempBranchPrefix: "temp",
			},
			expectedError: false,
			expectedStart: "temp_feature/branch_abcdef_",
		},
		{
			name: "BASE_REF exceeding 50 characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          strings.Repeat("a", 60),
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

func TestCommitAndPush_ForcePush(t *testing.T) {
	var capturedArgs []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "commit" {
				return "Files committed", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "push" {
				capturedArgs = args
				return nil
			}
			return nil
		},
	}

	config := &Config{
		ForcePush: true,
	}

	err := commitAndPush("test_branch", runner, config)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	expectedArgs := []string{"push", "--force", "origin", "test_branch"}
	if !slices.Equal(capturedArgs, expectedArgs) {
		t.Errorf("Expected push args %v, got %v", expectedArgs, capturedArgs)
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

	config := &Config{}

	err := commitAndPush("test_branch", runner, config)
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

	config := &Config{}

	err := commitAndPush("test_branch", runner, config)
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

	config := &Config{}

	err := commitAndPush("test_branch", runner, config)
	if err == nil {
		t.Errorf("Expected error, but got nil")
	} else if !strings.Contains(err.Error(), "push failed") {
		t.Errorf("Expected push error, but got %v", err)
	}
}

func TestResolveRealBase_UsesProvidedBase(t *testing.T) {
	runner := &MockCommandRunner{} // no calls expected
	cfg := &Config{BaseRef: "feature/xyz"}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "feature/xyz" {
		t.Fatalf("want feature/xyz, got %s", got)
	}
}

func TestResolveRealBase_FallbackToRemoteHEAD(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 2 && args[0] == "remote" && args[1] == "show" {
				// minimal realistic snippet
				return `
* remote origin
  Fetch URL: git@github.com:org/repo.git
  HEAD branch: develop
  Remote branches:
    develop tracked
    main    tracked
`, nil
			}
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: "123/merge"} // synthetic

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "develop" {
		t.Fatalf("want develop, got %s", got)
	}
}

func TestResolveRealBase_FallbackToMainWhenUnknown(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			// simulate git output that doesn't include "HEAD branch:"
			return "some weird output", nil
		},
	}
	cfg := &Config{BaseRef: ""} // empty → synthetic

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "main" {
		t.Fatalf("want main, got %s", got)
	}
}

func TestIsSyntheticRef(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"merge", true},
		{"head", true},
		{"123/merge", true},
		{"123/head", true},
		{"refs/pull/45/merge", true},
		{"refs/pull/45/head", true},
		{"feature/foo", false},
		{"main", false},
	}
	for _, c := range cases {
		if got := isSyntheticRef(c.in); got != c.want {
			t.Errorf("isSyntheticRef(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

func TestEnvVarsToConfig_BaseRefNormalization(t *testing.T) {
	// scrub
	keys := []string{"GITHUB_ACTOR", "GITHUB_SHA", "BASE_REF", "TEMP_BRANCH_PREFIX", "TRANSLATIONS_PATH", "BASE_LANG", "FLAT_NAMING", "ALWAYS_PULL_BASE", "FORCE_PUSH"}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	t.Setenv("GITHUB_ACTOR", "actor")
	t.Setenv("GITHUB_SHA", "abcdef")
	t.Setenv("BASE_REF", "refs/heads/release/2025-10")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "messages")
	t.Setenv("FILE_FORMAT", "json")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "false")
	t.Setenv("FORCE_PUSH", "false")

	cfg, err := envVarsToConfig()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.BaseRef != "release/2025-10" {
		t.Fatalf("want release/2025-10, got %s", cfg.BaseRef)
	}
}

func TestEnvVarsToConfig_NoExtsFails(t *testing.T) {
	// scrub
	keys := []string{"GITHUB_ACTOR", "GITHUB_SHA", "BASE_REF", "TEMP_BRANCH_PREFIX", "TRANSLATIONS_PATH", "BASE_LANG", "FLAT_NAMING", "ALWAYS_PULL_BASE", "FORCE_PUSH", "FILE_FORMAT", "FILE_EXT"}
	for _, k := range keys {
		t.Setenv(k, "")
	}

	t.Setenv("GITHUB_ACTOR", "actor")
	t.Setenv("GITHUB_SHA", "abcdef")
	t.Setenv("BASE_REF", "main")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "messages")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "false")
	t.Setenv("FORCE_PUSH", "false")

	_, err := envVarsToConfig()
	if err == nil || !strings.Contains(err.Error(), "cannot infer file extension") {
		t.Fatalf("expected missing ext error, got %v", err)
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
	sortedA := slices.Clone(a) // Create a copy to avoid modifying the original slices
	sortedB := slices.Clone(b)
	slices.Sort(sortedA)
	slices.Sort(sortedB)

	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}
