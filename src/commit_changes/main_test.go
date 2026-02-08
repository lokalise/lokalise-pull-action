package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type MockCommandRunner struct {
	RunFunc     func(name string, args ...string) error
	CaptureFunc func(name string, args ...string) (string, error)
}

func (m *MockCommandRunner) Run(name string, args ...string) error {
	if m.RunFunc != nil {
		return m.RunFunc(name, args...)
	}
	return nil
}

func (m *MockCommandRunner) Capture(name string, args ...string) (string, error) {
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
	// helper: by default pretend remote branch does NOT exist
	captureNoRemote := func(name string, args ...string) (string, error) {
		if name != "git" {
			return "", fmt.Errorf("unexpected binary: %s", name)
		}
		if len(args) >= 1 && args[0] == "ls-remote" {
			// hasRemote() must return false in these tests
			return "", fmt.Errorf("not found")
		}
		// allow fetch noise
		if len(args) >= 1 && args[0] == "fetch" {
			return "", nil
		}
		return "", nil
	}

	t.Run("creates from origin base", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				// allow upstream/unset-upstream calls if any
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "new_branch" && args[3] == "origin/main":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("new_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("falls back to local base", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "branch_from_local" && args[3] == "origin/dev":
					return fmt.Errorf("remote base missing")
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "branch_from_local" && args[3] == "dev":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("branch_from_local", "dev", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("switches to existing branch", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "existing_branch" && args[3] == "origin/main":
					return fmt.Errorf("already exists")
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "existing_branch" && args[3] == "main":
					return fmt.Errorf("already exists")
				case len(args) == 2 && args[0] == "checkout" && args[1] == "existing_branch":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("existing_branch", "main", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("updates existing PR head branch (uses headRef)", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: captureNoRemote,
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}
				// checkout -B new_br origin/new_br
				if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == "new_br" && args[3] == "origin/new_br" {
					return nil
				}
				return fmt.Errorf("unexpected command: git %v", args)
			},
		}

		if err := checkoutBranch("new_br", "main", "new_br", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("reuses existing remote branch; local changes differ -> stash, checkout, apply_stash_overwrite, drop", func(t *testing.T) {
		exitErr1 := func() error {
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.Command("cmd", "/C", "exit", "1")
			} else {
				cmd = exec.Command("sh", "-c", "exit 1")
			}
			return cmd.Run()
		}()

		var checkoutAttempts int
		var restored []string
		dropped := false

		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) >= 1 && args[0] == "ls-remote" {
					return "deadbeef\trefs/heads/lokalise-sync\n", nil
				}

				if len(args) >= 1 && args[0] == "fetch" {
					return "", nil
				}

				// worktreeEqualsRef(): different
				if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/lokalise-sync" {
					return "", exitErr1
				}
				if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/lokalise-sync" {
					return "", exitErr1
				}

				// status --porcelain (и тут, и внутри stashIfDirty)
				if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil
				}

				// list files in stash
				if len(args) == 5 &&
					args[0] == "stash" && args[1] == "show" &&
					args[2] == "--name-only" &&
					args[3] == "--include-untracked" &&
					args[4] == "stash@{0}" {
					return "locales/fr.json\n", nil
				}

				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}

				switch {
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "lokalise-sync" && args[3] == "origin/lokalise-sync":
					checkoutAttempts++
					if checkoutAttempts == 1 {
						return fmt.Errorf("Your local changes would be overwritten by checkout")
					}
					return nil

				case len(args) == 5 &&
					args[0] == "stash" && args[1] == "push" &&
					args[2] == "-u" && args[3] == "-m" && args[4] == "lokalise-temp":
					return nil

				// git checkout stash@{0} -- <file>
				case len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--":
					restored = append(restored, args[3])
					return nil

				// git checkout stash@{0}^3 -- <file>  (untracked)
				case len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--":
					restored = append(restored, args[3])
					return nil

				// drop stash
				case (len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}") ||
					(len(args) == 2 && args[0] == "stash" && args[1] == "drop"):
					dropped = true
					return nil

				case len(args) == 1 && args[0] == "reset":
					return nil
				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("lokalise-sync", "master", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !slices.Contains(restored, "locales/fr.json") {
			t.Fatalf("expected locales/fr.json to be restored from stash, got %v", restored)
		}
		if !dropped {
			t.Fatalf("expected stash to be dropped")
		}
	})

	t.Run("reuses existing remote branch; local changes block checkout but are identical -> force checkout", func(t *testing.T) {
		runner := &MockCommandRunner{
			CaptureFunc: func(name string, args ...string) (string, error) {
				if name != "git" {
					return "", fmt.Errorf("unexpected binary: %s", name)
				}

				// hasRemote(): remote branch exists
				if len(args) >= 1 && args[0] == "ls-remote" {
					return "deadbeef\trefs/heads/lokalise-sync\n", nil
				}

				// allow fetch noise
				if len(args) >= 1 && args[0] == "fetch" {
					return "", nil
				}

				// repo is dirty (so checkoutRemoteWithLocalChanges won't return cause)
				if len(args) >= 2 && args[0] == "status" && args[1] == "--porcelain" {
					return " M locales/fr.json\n", nil // no "?? " => no untracked
				}

				// worktreeEqualsRef(): identical -> diff exit code 0 (nil error)
				if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/lokalise-sync" {
					return "", nil
				}
				if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/lokalise-sync" {
					return "", nil
				}

				return "", nil
			},
			RunFunc: func(name string, args ...string) error {
				if name != "git" {
					return fmt.Errorf("unexpected binary: %s", name)
				}

				// allow upstream set
				if len(args) >= 1 && args[0] == "branch" {
					return nil
				}

				switch {
				// first try: normal checkout fails
				case len(args) == 4 &&
					args[0] == "checkout" && args[1] == "-B" &&
					args[2] == "lokalise-sync" && args[3] == "origin/lokalise-sync":
					return fmt.Errorf("Your local changes would be overwritten by checkout")

				// then we expect force checkout
				case len(args) == 5 &&
					args[0] == "checkout" && args[1] == "-f" && args[2] == "-B" &&
					args[3] == "lokalise-sync" && args[4] == "origin/lokalise-sync":
					return nil

				default:
					return fmt.Errorf("unexpected command: git %v", args)
				}
			},
		}

		if err := checkoutBranch("lokalise-sync", "master", "", runner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestCheckoutBranch_RemoteBranch_StashRestore_MultipleFiles(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	var restored []string
	dropped := false
	upstreamSet := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// hasRemote() true
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}

			// fetch noise
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}

			// status dirty
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n M i18n/fr.json\n", nil
			}

			// diff says: different
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}

			// stash show lists multiple files
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\ni18n/fr.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			// normal checkout fails first time, succeeds second (after stash)
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("Your local changes would be overwritten by checkout")
				}
				return nil
			}

			// stash push
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "-u" && args[3] == "-m" && args[4] == "lokalise-temp" {
				return nil
			}

			if len(args) == 1 && args[0] == "reset" {
				return nil
			}

			// restore from stash
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				restored = append(restored, args[3])
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" {
				restored = append(restored, args[3])
				return nil
			}

			// drop stash
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				dropped = true
				return nil
			}

			// set upstream
			if len(args) == 3 && args[0] == "branch" && strings.HasPrefix(args[1], "--set-upstream-to=origin/") && args[2] == branch {
				upstreamSet = true
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(restored, "locales/fr.json") || !slices.Contains(restored, "i18n/fr.json") {
		t.Fatalf("expected both files restored, got %v", restored)
	}
	if !dropped {
		t.Fatalf("expected stash to be dropped")
	}
	if !upstreamSet {
		t.Fatalf("expected upstream to be set")
	}
}

func TestCheckoutBranch_RemoteBranch_StashShowFails_NoDrop(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			// stash show fails
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "nope", fmt.Errorf("stash show failed")
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" {
				dropped = true
				return nil
			}

			// upstream set might still be attempted by outer function only on success, so ignore branch calls
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			// any restore/drop should NOT happen after stash show failure (restore won't happen anyway)
			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to list stashed files") {
		t.Fatalf("expected stash show error, got %v", err)
	}
	if dropped {
		t.Fatalf("stash must not be dropped on stash show failure")
	}
}

func TestCheckoutBranch_RemoteBranch_RestoreFails_NoDrop_StopsEarly(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false
	secondRestoreAttempted := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n M i18n/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}

			// ✅ ВОТ ЭТО КЛЮЧЕВО: include-untracked
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\ni18n/fr.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			// first restore fails
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "locales/fr.json" {
				return fmt.Errorf("restore failed")
			}

			// second restore must NOT happen
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "i18n/fr.json" {
				secondRestoreAttempted = true
				return nil
			}

			// first restore fails
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "locales/fr.json" {
				return fmt.Errorf("restore failed")
			}

			// second restore must NOT happen
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "i18n/fr.json" {
				secondRestoreAttempted = true
				return nil
			}

			// drop must NOT happen
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" {
				dropped = true
				return nil
			}

			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to restore locales/fr.json") {
		t.Fatalf("expected restore error, got %v", err)
	}
	if dropped {
		t.Fatalf("stash must not be dropped on restore failure")
	}
	if secondRestoreAttempted {
		t.Fatalf("expected restore loop to stop on first failure")
	}
}

func TestCheckoutBranch_RemoteBranch_DropFails_ReturnsError(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	restoredAny := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" &&
				args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "locales/fr.json\n", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}

			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				restoredAny = true
				return nil
			}

			// drop fails
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				return fmt.Errorf("drop failed")
			}

			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			return nil
		},
	}

	err := checkoutBranch(branch, base, "", runner)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to drop stash@{0}") {
		t.Fatalf("expected drop error, got %v", err)
	}
	if !restoredAny {
		t.Fatalf("expected at least one file to be restored before drop")
	}
}

func TestCheckoutBranch_RemoteBranch_EmptyStashFileList_StillDropsAndSucceeds(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "master"

	exitErr1 := func() error {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", "exit", "1")
		} else {
			cmd = exec.Command("sh", "-c", "exit 1")
		}
		return cmd.Run()
	}()

	var checkoutAttempts int
	dropped := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return " M locales/fr.json\n", nil
			}
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "origin/"+branch {
				return "", exitErr1
			}
			if len(args) >= 4 && args[0] == "diff" && args[1] == "--quiet" && args[2] == "--cached" && args[3] == "origin/"+branch {
				return "", exitErr1
			}
			// empty list
			if len(args) == 4 && args[0] == "stash" && args[1] == "show" && args[2] == "--name-only" && args[3] == "stash@{0}" {
				return "\n", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == branch && args[3] == "origin/"+branch {
				checkoutAttempts++
				if checkoutAttempts == 1 {
					return fmt.Errorf("blocked")
				}
				return nil
			}
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" {
				return nil
			}

			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" {
				return fmt.Errorf("restore should not be called on empty stash list")
			}
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				dropped = true
				return nil
			}
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}
			return nil
		},
	}

	if err := checkoutBranch(branch, base, "", runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dropped {
		t.Fatalf("expected stash to be dropped")
	}
}

func TestCheckoutBranch_FetchesCorrectRefspec(t *testing.T) {
	fetched := ""

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// make hasRemote() return false for branchName
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", fmt.Errorf("not found")
			}

			if len(args) >= 1 && args[0] == "fetch" {
				fetched = strings.Join(args, " ")
				return "", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			// allow branch noise
			if len(args) >= 1 && args[0] == "branch" {
				return nil
			}

			if len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == "new_branch" && args[3] == "origin/main" {
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

func TestCheckoutBranch_OverrideBranchExistsOnRemote(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "main"

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// hasRemote(...) -> ls-remote --exit-code --heads origin <branch>
			if len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" &&
				args[4] == branch {
				return "deadbeef\trefs/heads/" + branch + "\n", nil
			}

			// fetch --no-tags --prune origin +refs/heads/<branch>:refs/remotes/origin/<branch>
			if len(args) == 5 &&
				args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" &&
				args[3] == "origin" &&
				strings.HasPrefix(args[4], "+refs/heads/"+branch) &&
				strings.HasSuffix(args[4], ":refs/remotes/origin/"+branch) {
				return "", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			switch {
			// checkout -B lokalise-sync origin/lokalise-sync
			case len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == branch && args[3] == "origin/"+branch:
				return nil

			// branch --set-upstream-to=origin/lokalise-sync lokalise-sync   (== 3 args)
			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--set-upstream-to=origin/"+branch &&
				args[2] == branch:
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckoutBranch_OverrideMissing_FallbackToOriginBase_UnsetUpstream(t *testing.T) {
	const branch = "lokalise-sync"
	const base = "main"

	failedFirstRemoteCheckout := false
	var fetches []string

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// hasRemote() -> ls-remote ... branch
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", fmt.Errorf("not found")
			}

			// fetch --no-tags --prune origin <refspec>  (== 5 args)
			if len(args) == 5 && args[0] == "fetch" {
				fetches = append(fetches, strings.Join(args, " "))
				return "", nil
			}

			return "", fmt.Errorf("unexpected capture: git %v", args)
		},
		RunFunc: func(name string, args ...string) error {
			switch {
			case len(args) == 4 && args[0] == "checkout" && args[2] == branch && args[3] == "origin/"+branch && !failedFirstRemoteCheckout:
				failedFirstRemoteCheckout = true
				return fmt.Errorf("missing remote branch")

			case len(args) == 4 && args[0] == "checkout" && args[2] == branch && args[3] == "origin/"+base:
				return nil

			case len(args) == 2 && args[0] == "branch" && args[1] == "--unset-upstream":
				return nil
			}
			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, "", &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fetches) == 0 {
		t.Fatalf("expected fetch to be called at least once")
	}
}

func TestCheckoutBranch_HeadRefMatches_UsesRemoteHeadAndSetsUpstream(t *testing.T) {
	const branch = "feature/x"
	const base = "main"
	const head = "feature/x"

	runner := MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected binary: %s", name)
			}

			// make hasRemote(branchName) == false so code goes to headRef branch
			if len(args) >= 1 && args[0] == "ls-remote" {
				return "", fmt.Errorf("not found")
			}

			// allow fetch noise
			if len(args) >= 1 && args[0] == "fetch" {
				return "", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				return fmt.Errorf("unexpected binary: %s", name)
			}

			switch {
			// expect checkout from origin/headRef (success)
			case len(args) == 4 &&
				args[0] == "checkout" && args[1] == "-B" &&
				args[2] == branch && args[3] == "origin/"+head:
				return nil

			// set upstream to origin/headRef
			case len(args) == 3 &&
				args[0] == "branch" &&
				args[1] == "--set-upstream-to=origin/"+head &&
				args[2] == branch:
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	if err := checkoutBranch(branch, base, head, &runner); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCommitAndPushChanges_ForcePush_UsesForceWithLease(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "true")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "msg")

	var branchName string
	var pushArgs []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			if len(args) == 5 && args[0] == "ls-remote" {
				return "", fmt.Errorf("not found")
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/en.json\n", nil
			}
			if len(args) >= 1 && args[0] == "commit" {
				return "ok", nil
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				branchName = args[2]
				return nil
			}
			if len(args) >= 2 && args[0] == "add" && args[1] == "--" {
				return nil
			}
			if len(args) == 2 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}
			if len(args) >= 1 && args[0] == "push" {
				pushArgs = args
				return nil
			}
			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	want := []string{"push", "--force-with-lease", "origin", branchName}
	if !slices.Equal(pushArgs, want) {
		t.Fatalf("push args mismatch: want %v got %v", want, pushArgs)
	}
}

func TestCommitAndPushChanges_HappyPath_NoForce_NoOverride(t *testing.T) {
	// env
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")
	t.Setenv("GIT_COMMIT_MESSAGE", "Translations update")

	var branchName string
	var pushed []string
	commitCalled := false
	diffCalled := false
	addCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// hasRemote(branchName) -> false (no override remote branch)
			if len(args) == 5 && args[0] == "ls-remote" && args[1] == "--exit-code" && args[2] == "--heads" && args[3] == "origin" {
				return "", fmt.Errorf("not found")
			}

			// fetch main
			if len(args) == 5 && args[0] == "fetch" && args[1] == "--no-tags" && args[2] == "--prune" && args[3] == "origin" {
				return "", nil
			}

			// staged diff -> has changes
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				diffCalled = true
				return "locales/fr.json\n", nil
			}

			// commit
			if len(args) >= 1 && args[0] == "commit" {
				commitCalled = true

				found := false
				for i := 0; i+1 < len(args); i++ {
					if args[i] == "-m" && args[i+1] == "Translations update" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected commit message, got args: %v", args)
				}

				return "ok", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// git config --global user.*
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			// checkout -B <branch> origin/main
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[3] == "origin/main" {
				branchName = args[2]
				if !strings.HasPrefix(branchName, "lok_main_123456_") {
					t.Fatalf("unexpected branch name: %q", branchName)
				}
				return nil
			}

			// unset upstream
			if len(args) == 2 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}

			// git add -- ...
			if len(args) >= 2 && args[0] == "add" && args[1] == "--" {
				addCalled = true
				return nil
			}

			// push origin <branch>
			if len(args) == 3 && args[0] == "push" && args[1] == "origin" {
				pushed = args
				if args[2] != branchName {
					t.Fatalf("push branch mismatch: want %q got %q", branchName, args[2])
				}
				return nil
			}

			return fmt.Errorf("unexpected run: git %v", args)
		},
	}

	gotBranch, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if gotBranch != branchName {
		t.Fatalf("branch mismatch: got %q want %q", gotBranch, branchName)
	}
	if !diffCalled || !commitCalled || !addCalled {
		t.Fatalf("expected diff+commit+add; got diff=%v commit=%v add=%v", diffCalled, commitCalled, addCalled)
	}
	if len(pushed) == 0 {
		t.Fatalf("expected push")
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

func TestCommitAndPushChanges_NoChanges_ReturnsErrNoChanges_NoPush(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "main")

	pushCalled := false

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if len(args) == 5 && args[0] == "ls-remote" {
				return "", fmt.Errorf("not found")
			}
			if len(args) == 5 && args[0] == "fetch" {
				return "", nil
			}
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "", nil // no staged changes
			}
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" {
				return nil
			}
			if len(args) >= 2 && args[0] == "add" && args[1] == "--" {
				return nil
			}
			if len(args) == 2 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}
			if len(args) >= 1 && args[0] == "push" {
				pushCalled = true
				return nil
			}
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != ErrNoChanges {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}
	if pushCalled {
		t.Fatalf("push must not be called when no staged changes")
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

func TestCommitAndPushChanges_SyntheticBase_UsesRemoteHEAD(t *testing.T) {
	t.Setenv("GITHUB_ACTOR", "test_actor")
	t.Setenv("GITHUB_SHA", "1234567890abcdef")
	t.Setenv("TEMP_BRANCH_PREFIX", "lok")
	t.Setenv("TRANSLATIONS_PATH", "locales")
	t.Setenv("BASE_LANG", "en")
	t.Setenv("FLAT_NAMING", "true")
	t.Setenv("ALWAYS_PULL_BASE", "true")
	t.Setenv("FORCE_PUSH", "false")
	t.Setenv("FILE_EXT", "json")
	t.Setenv("BASE_REF", "123/merge") // synthetic

	var fetched []string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// resolveRealBase: git ls-remote --symref origin HEAD  (len == 4!)
			if len(args) == 4 &&
				args[0] == "ls-remote" &&
				args[1] == "--symref" &&
				args[2] == "origin" &&
				args[3] == "HEAD" {
				return "ref: refs/heads/master\tHEAD\n012345\tHEAD\n", nil
			}

			// hasRemote(branchName): not found
			if len(args) == 5 &&
				args[0] == "ls-remote" &&
				args[1] == "--exit-code" &&
				args[2] == "--heads" &&
				args[3] == "origin" {
				return "", fmt.Errorf("not found")
			}

			// fetch capture
			if len(args) == 5 && args[0] == "fetch" {
				fetched = append(fetched, args[4])
				return "", nil
			}

			// staged diff -> has changes
			if len(args) == 3 && args[0] == "diff" && args[1] == "--name-only" && args[2] == "--cached" {
				return "locales/fr.json\n", nil
			}

			// commit ok
			if len(args) >= 1 && args[0] == "commit" {
				return "ok", nil
			}

			t.Fatalf("unexpected capture: git %v", args)
			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				return nil
			}

			// checkout must be from origin/master
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" {
				if args[3] != "origin/master" {
					t.Fatalf("expected checkout from origin/master, got %q", args[3])
				}
				return nil
			}

			if len(args) == 2 && args[0] == "branch" && args[1] == "--unset-upstream" {
				return nil
			}
			if len(args) >= 2 && args[0] == "add" && args[1] == "--" {
				return nil
			}
			if len(args) >= 1 && args[0] == "push" {
				return nil
			}

			t.Fatalf("unexpected run: git %v", args)
			return nil
		},
	}

	_, err := commitAndPushChanges(runner)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	foundMaster := false
	for _, refspec := range fetched {
		if strings.Contains(refspec, "+refs/heads/master:refs/remotes/origin/master") {
			foundMaster = true
			break
		}
	}
	if !foundMaster {
		t.Fatalf("expected fetch refspec for master, got %v", fetched)
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
		name            string
		config          *Config
		expectedError   bool
		expectedStart   string // prefix for generated names; exact for overrides
		expectValidator bool   // whether we expect git check-ref-format to be called
		expectExact     bool   // whether to check exact match instead of prefix
	}{
		{
			name: "Valid inputs",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "feature_branch",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_feature_branch_123456_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "Valid inputs with branch override (simple)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "custom_branch",
			},
			expectedError:   false,
			expectedStart:   "custom_branch",
			expectValidator: true,
			expectExact:     true, // override should be returned as-is
		},
		{
			name: "Valid inputs with branch override (keeps + and other valid chars)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "feature/foo+bar",
			},
			expectedError:   false,
			expectedStart:   "feature/foo+bar",
			expectValidator: true,
			expectExact:     true,
		},
		{
			name: "Invalid override branch (space) should fail validation",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "bad branch",
			},
			expectedError:   true,
			expectValidator: true,
		},
		{
			name: "GITHUB_SHA too short",
			config: &Config{
				GitHubSHA:        "123",
				BaseRef:          "main",
				TempBranchPrefix: "temp",
			},
			expectedError:   true,
			expectValidator: false, // should error before validation call
		},
		{
			name: "BASE_REF with invalid characters (sanitized in generated name)",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "feature/branch!@#",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_feature/branch_abcdef_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "BASE_REF exceeding 50 characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          strings.Repeat("a", 60),
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_" + strings.Repeat("a", 50) + "_abcdef_",
			expectValidator: true,
			expectExact:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validated []string

			// simple validator stub: fail only on the specific invalid override we test
			runner := &MockCommandRunner{
				CaptureFunc: func(name string, args ...string) (string, error) {
					if name != "git" {
						return "", errors.New("unexpected command: " + name)
					}
					if len(args) != 3 || args[0] != "check-ref-format" || args[1] != "--branch" {
						return "", errors.New("unexpected args: " + strings.Join(args, " "))
					}
					branch := args[2]
					validated = append(validated, branch)

					// emulate git validation failure for a known-bad branch
					if branch == "bad branch" {
						return "fatal: invalid branch name", errors.New("exit status 1")
					}
					return "", nil
				},
			}

			branchName, err := generateBranchName(tt.config, runner)

			if tt.expectedError {
				if err == nil {
					t.Fatalf("expected error but got nil (branchName=%q)", branchName)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if tt.expectExact {
					if branchName != tt.expectedStart {
						t.Fatalf("expected branch name %q, got %q", tt.expectedStart, branchName)
					}
				} else {
					if !strings.HasPrefix(branchName, tt.expectedStart) {
						t.Fatalf("expected branch name to start with %q, got %q", tt.expectedStart, branchName)
					}
				}
			}

			// Assert validator call behavior
			if tt.expectValidator {
				if len(validated) == 0 {
					t.Fatalf("expected git check-ref-format to be called, but it wasn't")
				}
				// For successful cases, the validated name should match returned branchName
				if !tt.expectedError {
					last := validated[len(validated)-1]
					if last != branchName {
						t.Fatalf("expected validated branch %q to equal returned branch %q", last, branchName)
					}
				}
			} else {
				if len(validated) != 0 {
					t.Fatalf("did not expect validation call, but got %d call(s): %v", len(validated), validated)
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

func TestResolveRealBase_LsRemoteWins(t *testing.T) {
	// ls-remote --symref provides the default branch → should be used first
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				// Note the CRLF and tabs; real git may spit CRLF on Windows.
				return "ref: refs/heads/develop\tHEAD\r\n0123456789abcdef\tHEAD\r\n", nil
			}
			// should not be called, but keep harmless
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

func TestResolveRealBase_SymbolicRefFallback(t *testing.T) {
	// ls-remote fails → symbolic-ref gives "origin/main"
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				return "", fmt.Errorf("boom") // simulate network issue or no symref
			}
			if len(args) >= 4 && args[0] == "symbolic-ref" && args[1] == "--quiet" &&
				args[2] == "--short" && args[3] == "refs/remotes/origin/HEAD" {
				return "origin/main\n", nil
			}
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: ""}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "main" {
		t.Fatalf("want main, got %s", got)
	}
}

func TestResolveRealBase_RemoteShowAsLastNetworkFallback(t *testing.T) {
	// ls-remote fails, symbolic-ref fails, remote show origin returns HEAD branch
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				return "", fmt.Errorf("unexpected bin: %s", name)
			}
			if len(args) >= 3 && args[0] == "ls-remote" && args[1] == "--symref" && args[2] == "origin" {
				return "", fmt.Errorf("no symref here")
			}
			if len(args) >= 4 && args[0] == "symbolic-ref" && args[1] == "--quiet" &&
				args[2] == "--short" && args[3] == "refs/remotes/origin/HEAD" {
				return "", fmt.Errorf("no local origin/HEAD")
			}
			if len(args) >= 2 && args[0] == "remote" && args[1] == "show" {
				return `
* remote origin
  Fetch URL: git@github.com:org/repo.git
  HEAD branch: release
  Remote branches:
    develop tracked
    release tracked
`, nil
			}
			return "", fmt.Errorf("unexpected capture: %s %v", name, args)
		},
	}
	cfg := &Config{BaseRef: "456/merge"}

	got, err := resolveRealBase(runner, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "release" {
		t.Fatalf("want release, got %s", got)
	}
}

func TestResolveRealBase_FallbackToMainWhenEverythingFails(t *testing.T) {
	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			return "", fmt.Errorf("nope")
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

func TestGetDefaultBranchFromLsRemote_CRLF_AndCutPrefix(t *testing.T) {
	// direct unit test for the helper: ensure CRLF and CutPrefix flow works
	out := "ref: refs/heads/qa\tHEAD\r\n123456\tHEAD\r\n"
	r := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			return out, nil
		},
	}
	br, ok := getDefaultBranchFromLsRemote(r)
	if !ok || br != "qa" {
		t.Fatalf("want qa/true, got %q/%v", br, ok)
	}
}

func TestCheckoutRemoteWithLocalChanges_UntrackedRestoredFromThirdParent(t *testing.T) {
	const ref = "lokalise-sync"

	var calls [][]string

	runner := &MockCommandRunner{
		CaptureFunc: func(name string, args ...string) (string, error) {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}

			// dirty with untracked
			if len(args) == 2 && args[0] == "status" && args[1] == "--porcelain" {
				return "?? newfile.json\n", nil
			}

			// worktreeEqualsRef -> pretend "different"
			if len(args) >= 3 && args[0] == "diff" && args[1] == "--quiet" {
				// exit code 1 -> differences
				cmd := exec.Command("sh", "-c", "exit 1")
				if runtime.GOOS == "windows" {
					cmd = exec.Command("cmd", "/C", "exit", "1")
				}
				return "", cmd.Run()
			}

			// stash show includes untracked too
			if len(args) == 5 &&
				args[0] == "stash" && args[1] == "show" &&
				args[2] == "--name-only" && args[3] == "--include-untracked" &&
				args[4] == "stash@{0}" {
				return "newfile.json\n", nil
			}

			return "", nil
		},
		RunFunc: func(name string, args ...string) error {
			if name != "git" {
				t.Fatalf("unexpected binary: %s", name)
			}
			calls = append(calls, args)

			// stash push ok
			if len(args) == 5 && args[0] == "stash" && args[1] == "push" && args[2] == "-u" {
				return nil
			}

			// checkout branch ok
			if len(args) == 4 && args[0] == "checkout" && args[1] == "-B" && args[2] == ref && args[3] == "origin/"+ref {
				return nil
			}

			// restore: stash@{0} fails for untracked
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}" && args[2] == "--" && args[3] == "newfile.json" {
				return fmt.Errorf("not in tree")
			}
			// restore: ^3 succeeds
			if len(args) == 4 && args[0] == "checkout" && args[1] == "stash@{0}^3" && args[2] == "--" && args[3] == "newfile.json" {
				return nil
			}

			// reset ok
			if len(args) == 1 && args[0] == "reset" {
				return nil
			}

			// drop ok
			if len(args) == 3 && args[0] == "stash" && args[1] == "drop" && args[2] == "stash@{0}" {
				return nil
			}

			return nil
		},
	}

	cause := fmt.Errorf("blocked by local changes")
	err := checkoutRemoteWithLocalChanges(ref, runner, cause)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	found3 := false
	foundReset := false
	foundDrop := false

	for _, a := range calls {
		if slices.Equal(a, []string{"checkout", "stash@{0}^3", "--", "newfile.json"}) {
			found3 = true
		}
		if slices.Equal(a, []string{"reset"}) {
			foundReset = true
		}
		if slices.Equal(a, []string{"stash", "drop", "stash@{0}"}) {
			foundDrop = true
		}
	}

	if !found3 || !foundReset || !foundDrop {
		t.Fatalf("expected restore(^3)+reset+drop; got calls=%v", calls)
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
	return slices.Equal(a, b)
}
