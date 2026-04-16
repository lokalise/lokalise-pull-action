package main

import (
	"errors"
	"strings"
	"testing"
)

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
			name: "Override branch name is empty after trimming",
			config: &Config{
				GitHubSHA:          "1234567890abcdef",
				BaseRef:            "main",
				TempBranchPrefix:   "temp",
				OverrideBranchName: "   \t   ",
			},
			expectedError:   true,
			expectValidator: false,
		},
		{
			name: "Blank temp branch prefix falls back to lok",
			config: &Config{
				GitHubSHA:        "1234567890abcdef",
				BaseRef:          "main",
				TempBranchPrefix: "   ",
			},
			expectedError:   false,
			expectedStart:   "lok_main_123456_",
			expectValidator: true,
			expectExact:     false,
		},
		{
			name: "Sanitized empty base ref falls back to base",
			config: &Config{
				GitHubSHA:        "abcdef123456",
				BaseRef:          "!@#",
				TempBranchPrefix: "temp",
			},
			expectedError:   false,
			expectedStart:   "temp_base_abcdef_",
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
