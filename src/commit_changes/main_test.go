package main

import (
	"fmt"
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
				"HEAD_REF":             "refs/heads/feature/foo",
				"GITHUB_ACTOR":         "test_actor",
				"GITHUB_SHA":           "123456",
				"TEMP_BRANCH_PREFIX":   "temp",
				"TRANSLATIONS_PATH":    "translations",
				"FILE_FORMAT":          "json",
				"BASE_LANG":            "en",
				"FLAT_NAMING":          "true",
				"ALWAYS_PULL_BASE":     "false",
				"FORCE_PUSH":           "false",
				"GIT_USER_NAME":        "my_user",
				"GIT_USER_EMAIL":       "test@example.com",
				"OVERRIDE_BRANCH_NAME": "custom_branch",
				"GIT_COMMIT_MESSAGE":   "My commit msg",
			},
			expectedConfig: &Config{
				GitHubActor:        "test_actor",
				GitHubSHA:          "123456",
				BaseRef:            "main",
				HeadRef:            "feature/foo",
				TempBranchPrefix:   "temp",
				FileExt:            []string{"json"},
				BaseLang:           "en",
				FlatNaming:         true,
				AlwaysPullBase:     false,
				GitUserName:        "my_user",
				GitUserEmail:       "test@example.com",
				OverrideBranchName: "custom_branch",
				GitCommitMessage:   "My commit msg",
				TranslationPaths:   []string{"translations"},
				ForcePush:          false,
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
				"TRANSLATIONS_PATH":  "translations",
				"FILE_EXT":           "\n .JSON \n  yaml  \n .json \n",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "false",
				"ALWAYS_PULL_BASE":   "true",
				"FORCE_PUSH":         "false",
			},
			expectedConfig: &Config{
				GitHubActor:      "test_actor",
				GitHubSHA:        "123456",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
				FileExt:          []string{"json", "yaml"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   true,
				GitCommitMessage: "Translations update",
				TranslationPaths: []string{"translations"},
				ForcePush:        false,
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
				"TRANSLATIONS_PATH":    "translations",
				"FILE_FORMAT":          "json",
				"BASE_LANG":            "en",
				"FLAT_NAMING":          "true",
				"ALWAYS_PULL_BASE":     "false",
				"FORCE_PUSH":           "false",
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
				TranslationPaths:   []string{"translations"},
				ForcePush:          false,
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
				"TRANSLATIONS_PATH":  "translations",
				"FILE_FORMAT":        "structured_json",
				"FILE_EXT":           "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
				"FORCE_PUSH":         "false",
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
				TranslationPaths: []string{"translations"},
				ForcePush:        false,
			},
			expectError: false,
		},
		{
			name: "Missing required environment variable",
			envVars: map[string]string{
				"GITHUB_SHA":         "123456",
				"BASE_REF":           "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations",
				"FILE_FORMAT":        "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
			},
			expectError:     true,
			expectedErrText: "GITHUB_ACTOR",
		},
		{
			name: "Missing required extension and format info",
			envVars: map[string]string{
				"GITHUB_SHA":         "123456",
				"GITHUB_ACTOR":       "test_actor",
				"BASE_REF":           "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "true",
				"ALWAYS_PULL_BASE":   "false",
				"FORCE_PUSH":         "false",
			},
			expectError:     true,
			expectedErrText: "cannot infer file extension",
		},
		{
			name: "Invalid boolean environment variable",
			envVars: map[string]string{
				"GITHUB_ACTOR":       "test_actor",
				"GITHUB_SHA":         "123456",
				"BASE_REF":           "main",
				"TEMP_BRANCH_PREFIX": "temp",
				"TRANSLATIONS_PATH":  "translations",
				"FILE_FORMAT":        "json",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "not_a_bool",
				"ALWAYS_PULL_BASE":   "true",
				"FORCE_PUSH":         "false",
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
				"TRANSLATIONS_PATH":  "translations\nlocales",
				"FILE_EXT":           "strings\nstringsdict",
				"BASE_LANG":          "en",
				"FLAT_NAMING":        "false",
				"ALWAYS_PULL_BASE":   "true",
				"FORCE_PUSH":         "false",
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
				TranslationPaths: []string{"translations", "locales"},
				ForcePush:        false,
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
				"HEAD_REF",
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
				t.Setenv(k, "")
			}

			for key, value := range tt.envVars {
				t.Setenv(key, value)
			}

			config, err := envVarsToConfig()

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if tt.expectedErrText != "" && !containsSubstring(err.Error(), tt.expectedErrText) {
					t.Fatalf("Expected error containing '%s', but got '%v'", tt.expectedErrText, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if config == nil {
				t.Fatalf("Expected config but got nil")
			}
			if !reflect.DeepEqual(config, tt.expectedConfig) {
				t.Fatalf("Expected config %+v but got %+v", *tt.expectedConfig, *config)
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

func TestCheckoutBranch_FetchesCorrectRefspec(t *testing.T) {
	fetched := ""
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && args[0] == "fetch" {
				fetched = strings.Join(args, " ")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "new_branch" && args[3] == "origin/main" {
				return nil
			}
			return fmt.Errorf("unexpected: %v", args)
		},
	}
	if err := checkoutBranch("new_branch", "main", "", runner); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fetched, "+refs/heads/main:refs/remotes/origin/main") {
		t.Errorf("unexpected fetch refspec: %q", fetched)
	}
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

func TestCommitAndPush_ForcePush(t *testing.T) {
	var capturedArgs []string
	diffCalled := false
	commitCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 1 && args[0] == "diff" {
				if len(args) >= 3 && args[1] == "--name-only" && args[2] == "--cached" {
					diffCalled = true
					return "locales/en.json\n", nil
				}
			}
			if name == "git" && len(args) >= 1 && args[0] == "commit" {
				commitCalled = true
				return "Files committed", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && len(args) >= 1 && args[0] == "push" {
				capturedArgs = args
				return nil
			}
			return nil
		},
	}

	config := &Config{ForcePush: true}

	err := commitAndPush("test_branch", runner, config)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	expectedArgs := []string{"push", "--force", "origin", "test_branch"}
	if !slices.Equal(capturedArgs, expectedArgs) {
		t.Fatalf("Expected push args %v, got %v", expectedArgs, capturedArgs)
	}
	if !diffCalled || !commitCalled {
		t.Fatalf("Expected both diff --cached and commit to be called")
	}
}

func TestCommitAndPush_Success(t *testing.T) {
	diffCalled := false
	commitCalled := false
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 1 && args[0] == "diff" {
				if len(args) >= 3 && args[1] == "--name-only" && args[2] == "--cached" {
					diffCalled = true
					return "locales/en.json\n", nil
				}
			}
			if name == "git" && args[0] == "commit" {
				commitCalled = true
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
	if !diffCalled || !commitCalled {
		t.Fatalf("Expected both diff --cached and commit to be called")
	}
}

func TestCommitAndPush_CommitError(t *testing.T) {
	diffCalled := false
	commitCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 1 && args[0] == "diff" {
				if len(args) >= 3 && args[1] == "--name-only" && args[2] == "--cached" {
					diffCalled = true
					return "locales/en.json\n", nil
				}
			}
			if name == "git" && args[0] == "commit" {
				commitCalled = true
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

	if !diffCalled || !commitCalled {
		t.Fatalf("Expected both diff --cached and commit to be called")
	}
}

func TestCommitAndPush_PushError(t *testing.T) {
	diffCalled := false
	commitCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 1 && args[0] == "diff" {
				if len(args) >= 3 && args[1] == "--name-only" && args[2] == "--cached" {
					diffCalled = true
					return "locales/en.json\n", nil
				}
			}
			if name == "git" && args[0] == "commit" {
				commitCalled = true
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

	if !diffCalled || !commitCalled {
		t.Fatalf("Expected both diff --cached and commit to be called")
	}
}

func TestCommitAndPush_NoStaged_ReturnsNoChanges(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "", nil
			}
			t.Fatalf("Unexpected Capture call: %s %v", name, args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			t.Fatalf("Run should not be called when no staged changes")
			return nil
		},
	}

	err := commitAndPush("branch", runner, &Config{})
	if err != ErrNoChanges {
		t.Fatalf("Expected ErrNoChanges, got %v", err)
	}
}

func TestCommitAndPush_CommitFails_NoPush(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name == "git" && len(args) >= 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/en.json\n", nil
			}
			if name == "git" && len(args) >= 1 && args[0] == "commit" {
				return "hook said nope", fmt.Errorf("commit hook failed")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name == "git" && len(args) >= 1 && args[0] == "push" {
				t.Fatalf("push must not be called when commit fails")
			}
			return nil
		},
	}

	err := commitAndPush("branch", runner, &Config{GitCommitMessage: "msg"})
	if err == nil || !strings.Contains(err.Error(), "failed to commit changes") {
		t.Fatalf("Expected commit error, got %v", err)
	}
}

func TestBuildGitAddArgs(t *testing.T) {
	J := func(parts ...string) string { return filepath.ToSlash(filepath.Join(parts...)) }

	tests := []struct {
		name         string
		config       *Config
		expectedArgs []string
	}{
		{
			name: "Flat naming with AlwaysPullBase = true, single path",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   true,
				TranslationPaths: []string{"path/to/translations"},
			},
			expectedArgs: []string{
				J("path", "to", "translations", "*.json"),
				":!" + J("path", "to", "translations", "**", "*.json"),
			},
		},
		{
			name: "Flat naming with AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   true,
				TranslationPaths: []string{"path1", "path2"},
			},
			expectedArgs: []string{
				J("path1", "*.json"),
				":!" + J("path1", "**", "*.json"),
				J("path2", "*.json"),
				":!" + J("path2", "**", "*.json"),
			},
		},
		{
			name: "Flat naming with AlwaysPullBase = false, multiple paths",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   false,
				TranslationPaths: []string{"path1", "path2"},
			},
			expectedArgs: []string{
				J("path1", "*.json"),
				":!" + J("path1", "en.json"),
				":!" + J("path1", "**", "*.json"),
				J("path2", "*.json"),
				":!" + J("path2", "en.json"),
				":!" + J("path2", "**", "*.json"),
			},
		},
		{
			name: "Nested naming with AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   true,
				TranslationPaths: []string{"path1", "path2"},
			},
			expectedArgs: []string{
				J("path1", "**", "*.json"),
				J("path2", "**", "*.json"),
			},
		},
		{
			name: "Nested naming with AlwaysPullBase = false, multiple paths",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   false,
				TranslationPaths: []string{"path1", "path2"},
			},
			expectedArgs: []string{
				J("path1", "**", "*.json"),
				":!" + J("path1", "en", "**"),
				J("path2", "**", "*.json"),
				":!" + J("path2", "en", "**"),
			},
		},
		{
			name: "Empty translations path",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   true,
				TranslationPaths: []string{},
			},
			expectedArgs: []string{},
		},
		{
			name: "Flat naming + multi-ext (iOS) + AlwaysPullBase = false, single path",
			config: &Config{
				FileExt:          []string{"strings", "stringsdict"},
				BaseLang:         "en",
				FlatNaming:       true,
				AlwaysPullBase:   false,
				TranslationPaths: []string{"ios/Localizations"},
			},
			expectedArgs: []string{
				// strings
				J("ios", "Localizations", "*.strings"),
				":!" + J("ios", "Localizations", "en.strings"),
				":!" + J("ios", "Localizations", "**", "*.strings"),
				// stringsdict
				J("ios", "Localizations", "*.stringsdict"),
				":!" + J("ios", "Localizations", "en.stringsdict"),
				":!" + J("ios", "Localizations", "**", "*.stringsdict"),
			},
		},
		{
			name: "Nested naming + multi-ext (iOS) + AlwaysPullBase = true, multiple paths",
			config: &Config{
				FileExt:          []string{"strings", "stringsdict"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   true,
				TranslationPaths: []string{"ios/ModuleA", "ios/ModuleB"},
			},
			expectedArgs: []string{
				// ModuleA both exts
				J("ios", "ModuleA", "**", "*.strings"),
				J("ios", "ModuleA", "**", "*.stringsdict"),
				// ModuleB both exts
				J("ios", "ModuleB", "**", "*.strings"),
				J("ios", "ModuleB", "**", "*.stringsdict"),
			},
		},
		{
			name: "Nested naming + multi-ext (iOS) + AlwaysPullBase = false (exclude base dir once per path)",
			config: &Config{
				FileExt:          []string{"strings", "stringsdict"},
				BaseLang:         "en",
				FlatNaming:       false,
				AlwaysPullBase:   false,
				TranslationPaths: []string{"ios/App"},
			},
			expectedArgs: []string{
				J("ios", "App", "**", "*.strings"),
				J("ios", "App", "**", "*.stringsdict"),
				":!" + J("ios", "App", "en", "**"),
			},
		},
		{
			name: "Flat naming + AlwaysPullBase = false, but BaseLang empty -> no base excludes",
			config: &Config{
				FileExt:          []string{"json"},
				BaseLang:         "",
				FlatNaming:       true,
				AlwaysPullBase:   false,
				TranslationPaths: []string{"p"},
			},
			expectedArgs: []string{
				J("p", "*.json"),
				":!" + J("p", "**", "*.json"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildGitAddArgs(tt.config)
			if !equalSlices(got, tt.expectedArgs) {
				t.Fatalf("buildGitAddArgs() = %v, want %v", got, tt.expectedArgs)
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
	t.Parallel() // this test can run alongside other tests

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
		{"pull/99/merge", true},
		{"feature/foo", false},
		{"main", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("in=%q", c.in), func(t *testing.T) {
			t.Parallel()

			got := isSyntheticRef(c.in)
			if got != c.want {
				t.Errorf("isSyntheticRef(%q) = %v; want %v", c.in, got, c.want)
			}
		})
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
