package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	// Override exitFunc for testing
	exitFunc = func(code int) {
		panic(fmt.Sprintf("Exit called with code %d", code))
	}

	// Run tests
	code := m.Run()

	// Restore exitFunc after testing (optional)
	exitFunc = os.Exit

	os.Exit(code)
}

func TestExecuteDownloadTimeout(t *testing.T) {
	// Define the path to the mock process binary
	mockBinary := "./fixtures/mock_sleep"
	if runtime.GOOS == "windows" {
		mockBinary += ".exe"
	}

	// Build the mock binary from the fixtures directory
	buildMockBinaryIfNeeded(t, "./fixtures/sleep.go", mockBinary)

	// Use the actual executeDownload function with the mock binary
	args := []string{"sleep"} // Argument to trigger sleep in the mock process
	downloadTimeout := 1      // Timeout in seconds, smaller than sleep duration

	fmt.Println("Testing executeDownload with a timeout...")
	outputBytes, err := executeDownload(mockBinary, args, downloadTimeout)
	fmt.Println("Execution completed.")

	// Assert that the error matches "command timed out"
	if err == nil {
		t.Errorf("Expected timeout error, but got nil")
	} else if err.Error() != "command timed out" {
		t.Errorf("Expected 'command timed out' error, but got: %v", err)
	}

	// Debug: Print captured output
	fmt.Printf("Output from mock binary: %s\n", string(outputBytes))
}

func TestExecuteDownloadNonTimeoutError(t *testing.T) {
	// Define the path to a non-existent binary to simulate an execution error
	nonExistentBinary := "./path/to/nonexistent/binary"

	// Use the actual executeDownload function
	args := []string{"arg1", "arg2"}
	downloadTimeout := 5 // Timeout in seconds

	fmt.Println("Testing executeDownload with a non-timeout error...")
	outputBytes, err := executeDownload(nonExistentBinary, args, downloadTimeout)
	fmt.Println("Execution completed.")

	// Assert that an error occurred
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	}

	// Debug: Print captured output
	fmt.Printf("Output from executeDownload: %s\n", string(outputBytes))
}

func TestValidateDownloadConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      DownloadConfig
		shouldError bool
	}{
		{
			name: "Valid config",
			config: DownloadConfig{
				ProjectID:     "test_project",
				Token:         "test_token",
				FileFormat:    "json",
				GitHubRefName: "main",
			},
			shouldError: false,
		},
		{
			name: "Missing ProjectID",
			config: DownloadConfig{
				Token:         "test_token",
				FileFormat:    "json",
				GitHubRefName: "main",
			},
			shouldError: true,
		},
		{
			name: "Missing Token",
			config: DownloadConfig{
				ProjectID:     "test_project",
				FileFormat:    "json",
				GitHubRefName: "main",
			},
			shouldError: true,
		},
		{
			name: "Missing FileFormat",
			config: DownloadConfig{
				ProjectID:     "test_project",
				Token:         "test_token",
				GitHubRefName: "main",
			},
			shouldError: true,
		},
		{
			name: "Missing GitHubRefName",
			config: DownloadConfig{
				ProjectID:  "test_project",
				Token:      "test_token",
				FileFormat: "json",
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if r := recover(); r != nil {
					if !tt.shouldError {
						t.Errorf("Unexpected panic for test '%s': %v", tt.name, r)
					}
				} else if tt.shouldError {
					t.Errorf("Expected an error for test '%s', but validation passed", tt.name)
				}
			}()
			validateDownloadConfig(tt.config)
		})
	}
}

func TestConstructDownloadArgs(t *testing.T) {
	tests := []struct {
		name         string
		config       DownloadConfig
		expectedArgs []string
	}{
		{
			name: "Include Tags with Single Additional Param",
			config: DownloadConfig{
				ProjectID:        "test_project",
				Token:            "test_token",
				FileFormat:       "json",
				GitHubRefName:    "main",
				AdditionalParams: "--custom-flag=true",
				SkipIncludeTags:  false,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--original-filenames=true",
				"--directory-prefix=/",
				"--include-tags=main",
				"--custom-flag=true",
			},
		},
		{
			name: "Skip Include Tags",
			config: DownloadConfig{
				ProjectID:        "test_project",
				Token:            "test_token",
				FileFormat:       "json",
				GitHubRefName:    "main",
				AdditionalParams: "--custom-flag=true",
				SkipIncludeTags:  true,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--original-filenames=true",
				"--directory-prefix=/",
				"--custom-flag=true",
			},
		},
		{
			name: "Skip Original Filenames",
			config: DownloadConfig{
				ProjectID:             "test_project",
				Token:                 "test_token",
				FileFormat:            "json",
				GitHubRefName:         "main",
				AdditionalParams:      "--custom-flag=true",
				SkipOriginalFilenames: true,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--include-tags=main",
				"--custom-flag=true",
			},
		},
		{
			name: "Async mode",
			config: DownloadConfig{
				ProjectID:             "test_project",
				Token:                 "test_token",
				FileFormat:            "json",
				GitHubRefName:         "main",
				SkipOriginalFilenames: true,
				AsyncMode:             true,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--async",
				"--include-tags=main",
			},
		},
		{
			name: "Empty Additional Params",
			config: DownloadConfig{
				ProjectID:        "test_project",
				Token:            "test_token",
				FileFormat:       "json",
				GitHubRefName:    "main",
				AdditionalParams: "",
				SkipIncludeTags:  false,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--original-filenames=true",
				"--directory-prefix=/",
				"--include-tags=main",
			},
		},
		{
			name: "Multiple Additional Params (YAML multiline style)",
			config: DownloadConfig{
				ProjectID:     "test_project",
				Token:         "test_token",
				FileFormat:    "json",
				GitHubRefName: "main",
				AdditionalParams: `
--custom-flag=true
--another-flag=false
--quoted=some value
--json={"key":"value with space"}
--empty-flag=
`,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--original-filenames=true",
				"--directory-prefix=/",
				"--include-tags=main",
				"--custom-flag=true",
				"--another-flag=false",
				"--quoted=some value",
				"--json={\"key\":\"value with space\"}",
				"--empty-flag=",
			},
		},
		{
			name: "JSON Array Additional Param",
			config: DownloadConfig{
				ProjectID:     "test_project",
				Token:         "test_token",
				FileFormat:    "json",
				GitHubRefName: "main",
				AdditionalParams: `
--language-mapping=[{"original_language_iso":"en_US","custom_language_iso":"en-US"},{"original_language_iso":"fr_CA","custom_language_iso":"fr-CA"}]
`,
			},
			expectedArgs: []string{
				"--token=test_token",
				"--project-id=test_project",
				"file", "download",
				"--format=json",
				"--original-filenames=true",
				"--directory-prefix=/",
				"--include-tags=main",
				"--language-mapping=[{\"original_language_iso\":\"en_US\",\"custom_language_iso\":\"en-US\"},{\"original_language_iso\":\"fr_CA\",\"custom_language_iso\":\"fr-CA\"}]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := constructDownloadArgs(tt.config)

			if !reflect.DeepEqual(actual, tt.expectedArgs) {
				t.Errorf("Arguments mismatch for '%s':\nExpected: %q\nActual:   %q",
					tt.name, tt.expectedArgs, actual)
			}
		})
	}
}

func TestDownloadFiles(t *testing.T) {
	tests := []struct {
		name         string
		config       DownloadConfig
		mockExecutor func(cmdPath string, args []string, timeout int) ([]byte, error)
		shouldError  bool
	}{
		{
			name: "Successful download",
			config: DownloadConfig{
				ProjectID:       "test_project",
				Token:           "test_token",
				FileFormat:      "json",
				GitHubRefName:   "main",
				MaxRetries:      3,
				SleepTime:       1,
				DownloadTimeout: 120,
			},
			mockExecutor: func(cmdPath string, args []string, timeout int) ([]byte, error) {
				return []byte("Download succeeded"), nil
			},
			shouldError: false,
		},
		{
			name: "Rate-limited and retries succeed",
			config: DownloadConfig{
				ProjectID:       "test_project",
				Token:           "test_token",
				FileFormat:      "json",
				GitHubRefName:   "main",
				MaxRetries:      3,
				SleepTime:       1,
				DownloadTimeout: 120,
			},
			mockExecutor: func() func(cmdPath string, args []string, timeout int) ([]byte, error) {
				callCount := 0
				return func(cmdPath string, args []string, timeout int) ([]byte, error) {
					callCount++
					if callCount == 1 {
						return []byte("API request error 429"), errors.New("rate limit")
					}
					return []byte("Download succeeded"), nil
				}
			}(),
			shouldError: false,
		},
		{
			name: "Permanent error",
			config: DownloadConfig{
				ProjectID:       "test_project",
				Token:           "test_token",
				FileFormat:      "json",
				GitHubRefName:   "main",
				MaxRetries:      3,
				SleepTime:       1,
				DownloadTimeout: 120,
			},
			mockExecutor: func(cmdPath string, args []string, timeout int) ([]byte, error) {
				return []byte("Unexpected error"), errors.New("permanent error")
			},
			shouldError: true,
		},
		{
			name: "No keys error",
			config: DownloadConfig{
				ProjectID:       "test_project",
				Token:           "test_token",
				FileFormat:      "json",
				GitHubRefName:   "main",
				MaxRetries:      3,
				SleepTime:       1,
				DownloadTimeout: 120,
			},
			mockExecutor: func(cmdPath string, args []string, timeout int) ([]byte, error) {
				return []byte("API request error 406"), errors.New("no keys")
			},
			shouldError: true,
		},
		{
			name: "Execution error with ambiguous output",
			config: DownloadConfig{
				ProjectID:       "test_project",
				Token:           "test_token",
				FileFormat:      "json",
				GitHubRefName:   "main",
				MaxRetries:      3,
				SleepTime:       1,
				DownloadTimeout: 120,
			},
			mockExecutor: func(cmdPath string, args []string, timeout int) ([]byte, error) {
				// Simulate an error but with no clear error message in output
				return []byte("Some unexpected CLI output with no errors"), errors.New("command failed")
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					if !tt.shouldError {
						t.Errorf("Unexpected error in test '%s': %v", tt.name, r)
					}
				} else if tt.shouldError {
					t.Errorf("Expected an error in test '%s' but did not get one", tt.name)
				}
			}()

			downloadFiles(tt.config, tt.mockExecutor)
		})
	}
}

// buildMockBinaryIfNeeded compiles the mock Go program if it's not already built or is outdated.
func buildMockBinaryIfNeeded(t *testing.T, sourcePath, outputPath string) {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("Failed to stat source file: %v", err)
	}

	binaryInfo, err := os.Stat(outputPath)
	if err == nil && binaryInfo.ModTime().After(sourceInfo.ModTime()) {
		// Binary exists and is newer than the source, no need to rebuild
		return
	}

	// Build the binary
	cmd := exec.Command("go", "build", "-o", outputPath, sourcePath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build mock binary: %v", err)
	}
}
