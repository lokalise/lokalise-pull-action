package main

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateBranchName(t *testing.T) {
	validationErr := errors.New("exit status 1")

	tests := []struct {
		name            string
		config          *Config
		expectedError   bool
		expectedCause   error
		expectedStart   string
		expectValidator bool
		expectExact     bool
	}{
		{
			name: "Valid inputs",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "feature_branch",
				TempBranchPrefix: "temp",
			},
			expectedStart:   "temp_feature_branch_123456_",
			expectValidator: true,
		},
		{
			name: "Override branch name is empty after trimming",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "main",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "   \t   ",
			},
			expectedError: true,
		},
		{
			name: "Blank temp branch prefix falls back to lok",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "main",
				TempBranchPrefix: "   ",
			},
			expectedStart:   "lok_main_123456_",
			expectValidator: true,
		},
		{
			name: "Sanitized empty base ref falls back to base",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "!@#",
				TempBranchPrefix: "temp",
			},
			expectedStart:   "temp_base_abcdef_",
			expectValidator: true,
		},
		{
			name: "Valid inputs with branch override (simple)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "custom_branch",
			},
			expectedStart:   "custom_branch",
			expectValidator: true,
			expectExact:     true,
		},
		{
			name: "Valid inputs with branch override (keeps + and other valid chars)",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "feature_branch",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "feature/foo+bar",
			},
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
			expectedCause:   validationErr,
			expectValidator: true,
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
			name: "BASE_REF with invalid characters (sanitized in generated name)",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "feature/branch!@#",
				TempBranchPrefix: "temp",
			},
			expectedStart:   "temp_feature/branch_abcdef_",
			expectValidator: true,
		},
		{
			name: "BASE_REF exceeding 50 characters",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          strings.Repeat("a", 60),
				TempBranchPrefix: "temp",
			},
			expectedStart:   "temp_" + strings.Repeat("a", 50) + "_abcdef_",
			expectValidator: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var validated []string

			runner := &MockCommandRunner{
				CaptureFunc: func(name string, args ...string) (string, error) {
					if name != "git" {
						return "", errors.New("unexpected command: " + name)
					}

					if len(args) != 3 ||
						args[0] != "check-ref-format" ||
						args[1] != "--branch" {
						return "", errors.New(
							"unexpected args: " + strings.Join(args, " "),
						)
					}

					branch := args[2]
					validated = append(validated, branch)

					if branch == "bad branch" {
						return "fatal: invalid branch name", validationErr
					}

					return "", nil
				},
			}

			branchName, err := generateBranchName(tt.config, runner)

			if tt.expectedError {
				if err == nil {
					t.Fatalf(
						"expected error but got nil (branchName=%q)",
						branchName,
					)
				}

				if tt.expectedCause != nil && !errors.Is(err, tt.expectedCause) {
					t.Fatalf(
						"expected error wrapping %v, got %v",
						tt.expectedCause,
						err,
					)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				if tt.expectExact {
					if branchName != tt.expectedStart {
						t.Fatalf(
							"expected branch name %q, got %q",
							tt.expectedStart,
							branchName,
						)
					}
				} else if !strings.HasPrefix(branchName, tt.expectedStart) {
					t.Fatalf(
						"expected branch name to start with %q, got %q",
						tt.expectedStart,
						branchName,
					)
				}
			}

			if tt.expectValidator {
				if len(validated) == 0 {
					t.Fatal(
						"expected git check-ref-format to be called, but it wasn't",
					)
				}

				if !tt.expectedError {
					last := validated[len(validated)-1]
					if last != branchName {
						t.Fatalf(
							"expected validated branch %q to equal returned branch %q",
							last,
							branchName,
						)
					}
				}
			} else if len(validated) != 0 {
				t.Fatalf(
					"did not expect validation call, but got %d call(s): %v",
					len(validated),
					validated,
				)
			}
		})
	}
}
