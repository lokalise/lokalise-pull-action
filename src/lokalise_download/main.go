package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// exitFunc is a function variable that defaults to os.Exit.
// This can be overridden in tests to capture exit behavior.
var exitFunc = os.Exit

const (
	defaultMaxRetries      = 5   // Default number of retries for rate-limited requests
	defaultSleepTime       = 1   // Default initial sleep time in seconds between retries
	maxSleepTime           = 60  // Maximum sleep time in seconds between retries
	maxTotalTime           = 300 // Maximum total time in seconds for all retries
	defaultDownloadTimeout = 120 // Timeout for the download script
)

// DownloadConfig holds all the necessary configuration for downloading files
type DownloadConfig struct {
	ProjectID             string
	Token                 string
	FileFormat            string
	GitHubRefName         string
	AdditionalParams      string
	SkipIncludeTags       bool
	SkipOriginalFilenames bool
	MaxRetries            int
	SleepTime             int
	DownloadTimeout       int
	AsyncMode             bool
}

func main() {
	// Ensure the required command-line arguments are provided
	if len(os.Args) < 3 {
		returnWithError("Usage: lokalise_download <project_id> <token>")
	}

	skipIncludeTags, err := parsers.ParseBoolEnv("SKIP_INCLUDE_TAGS")
	if err != nil {
		skipIncludeTags = false
	}
	skipOriginalFilenames, err := parsers.ParseBoolEnv("SKIP_ORIGINAL_FILENAMES")
	if err != nil {
		skipOriginalFilenames = false
	}
	asyncMode, err := parsers.ParseBoolEnv("ASYNC_MODE")
	if err != nil {
		asyncMode = false
	}

	// Create the download configuration
	config := DownloadConfig{
		ProjectID:             os.Args[1],
		Token:                 os.Args[2],
		FileFormat:            os.Getenv("FILE_FORMAT"),
		GitHubRefName:         os.Getenv("GITHUB_REF_NAME"),
		AdditionalParams:      os.Getenv("CLI_ADD_PARAMS"),
		SkipIncludeTags:       skipIncludeTags,
		SkipOriginalFilenames: skipOriginalFilenames,
		AsyncMode:             asyncMode,
		MaxRetries:            parsers.ParseUintEnv("MAX_RETRIES", defaultMaxRetries),
		SleepTime:             parsers.ParseUintEnv("SLEEP_TIME", defaultSleepTime),
		DownloadTimeout:       parsers.ParseUintEnv("DOWNLOAD_TIMEOUT", defaultDownloadTimeout),
	}

	// Validate the configuration
	validateDownloadConfig(config)

	// Start the download process
	downloadFiles(config, executeDownload)
}

// validateDownloadConfig ensures the configuration has all necessary fields
func validateDownloadConfig(config DownloadConfig) {
	if config.ProjectID == "" {
		returnWithError("Project ID is required and cannot be empty.")
	}
	if config.Token == "" {
		returnWithError("API token is required and cannot be empty.")
	}
	if config.FileFormat == "" {
		returnWithError("FILE_FORMAT environment variable is required.")
	}
	if config.GitHubRefName == "" {
		returnWithError("GITHUB_REF_NAME environment variable is required.")
	}
}

// executeDownload runs the lokalise2 CLI to download files with a timeout
func executeDownload(cmdPath string, args []string, downloadTimeout int) ([]byte, error) {
	timeout := time.Duration(downloadTimeout) * time.Second

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)

	outputBytes, err := cmd.CombinedOutput()

	// Check if the context timed out
	if ctx.Err() == context.DeadlineExceeded {
		return outputBytes, errors.New("command timed out")
	}

	return outputBytes, err
}

// constructDownloadArgs builds the arguments for the lokalise2 CLI tool
func constructDownloadArgs(config DownloadConfig) []string {
	args := []string{
		fmt.Sprintf("--token=%s", config.Token),
		fmt.Sprintf("--project-id=%s", config.ProjectID),
		"file", "download",
		fmt.Sprintf("--format=%s", config.FileFormat),
	}

	if config.AsyncMode {
		args = append(args, "--async")
	}

	if !config.SkipOriginalFilenames {
		args = append(args, "--original-filenames=true", "--directory-prefix=/")
	}

	if !config.SkipIncludeTags {
		args = append(args, fmt.Sprintf("--include-tags=%s", config.GitHubRefName))
	}

	if config.AdditionalParams != "" {
		scanner := bufio.NewScanner(strings.NewReader(config.AdditionalParams))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				args = append(args, line)
			}
		}
		if err := scanner.Err(); err != nil {
			returnWithError(fmt.Sprintf("Failed to parse additional parameters: %v", err))
		}
	}

	return args
}

// downloadFiles attempts to download files from Lokalise using the lokalise2 CLI tool.
// It handles rate limiting by retrying with exponential backoff and checks for specific API errors.
func downloadFiles(config DownloadConfig, downloadExecutor func(cmdPath string, args []string, timeout int) ([]byte, error)) {
	fmt.Println("Starting download from Lokalise")

	args := constructDownloadArgs(config)
	startTime := time.Now()
	sleepTime := config.SleepTime
	maxRetries := config.MaxRetries

	for attempt := 1; attempt <= config.MaxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d\n", attempt, maxRetries)

		outputBytes, err := downloadExecutor("./bin/lokalise2", args, config.DownloadTimeout)

		output := string(outputBytes)

		// Check for rate limit error (HTTP status code 429)
		if isRateLimitError(output) {
			if time.Since(startTime).Seconds() >= maxTotalTime {
				returnWithError(fmt.Sprintf("Max retry time exceeded; exiting after %d attempts.", attempt))
			}
			fmt.Printf("Rate limit error on attempt %d; retrying in %d seconds...\n", attempt, sleepTime)
			time.Sleep(time.Duration(sleepTime) * time.Second)
			sleepTime = min(sleepTime*2, maxSleepTime)
			continue
		}

		// Check for "no keys" error (HTTP status code 406)
		if isNoKeysError(output) {
			returnWithError("no keys for export with current settings; exiting")
		}

		if err == nil {
			fmt.Println("Successfully downloaded files.")
			return
		}

		// For any other errors, log the output and continue to the next attempt
		fmt.Fprintf(os.Stderr, "Unexpected error during download on attempt %d: %s\n", attempt, output)
	}

	returnWithError(fmt.Sprintf("Failed to download files after %d attempts.", config.MaxRetries))
}

// isRateLimitError checks if the output contains a rate limit error (HTTP status code 429).
func isRateLimitError(output string) bool {
	return strings.Contains(output, "API request error 429")
}

// isNoKeysError checks if the output contains a "no keys" error (HTTP status code 406).
func isNoKeysError(output string) bool {
	return strings.Contains(output, "API request error 406")
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// returnWithError prints an error message to stderr and exits the program with a non-zero status code.
func returnWithError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	exitFunc(1)
}
