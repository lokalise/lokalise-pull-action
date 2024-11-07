package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRetries = 5   // Default number of retries for rate-limited requests
	defaultSleepTime  = 1   // Default initial sleep time in seconds between retries
	maxSleepTime      = 60  // Maximum sleep time in seconds between retries
	maxTotalTime      = 300 // Maximum total time in seconds for all retries
)

func main() {
	// Ensure the required command-line arguments are provided
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: lokalise_download <project_id> <token>")
		os.Exit(1)
	}

	projectID := os.Args[1]
	token := os.Args[2]

	// Start the download process
	if err := downloadFiles(projectID, token); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// downloadFiles attempts to download files from Lokalise using the lokalise2 CLI tool.
// It handles rate limiting by retrying with exponential backoff and checks for specific API errors.
func downloadFiles(projectID, token string) error {
	// Validate required inputs
	if projectID == "" || token == "" {
		return fmt.Errorf("project_id and token are required and cannot be empty")
	}

	fmt.Println("Starting download from Lokalise")
	startTime := time.Now()

	// Retrieve retry configurations from environment variables
	maxRetries := getEnvAsInt("MAX_RETRIES", defaultMaxRetries)
	sleepTime := getEnvAsInt("SLEEP_TIME", defaultSleepTime)
	currentSleepTime := sleepTime

	// Retrieve additional configurations from environment variables
	cliAddParams := os.Getenv("CLI_ADD_PARAMS")
	fileFormat := os.Getenv("FILE_FORMAT")
	githubRefName := os.Getenv("GITHUB_REF_NAME")

	// Validate that required environment variables are set
	if fileFormat == "" {
		return fmt.Errorf("FILE_FORMAT environment variable is required")
	}
	if githubRefName == "" {
		return fmt.Errorf("GITHUB_REF_NAME environment variable is required")
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d\n", attempt, maxRetries)

		// Construct command arguments for the lokalise2 CLI tool
		cmdArgs := []string{
			fmt.Sprintf("--token=%s", token),
			fmt.Sprintf("--project-id=%s", projectID),
			"file", "download",
			fmt.Sprintf("--format=%s", fileFormat),
			"--original-filenames=true",
			"--directory-prefix=/",
			fmt.Sprintf("--include-tags=%s", githubRefName),
		}

		// Append any additional parameters specified in the environment variable
		if cliAddParams != "" {
			cmdArgs = append(cmdArgs, strings.Fields(cliAddParams)...)
		}

		// Execute the command and capture combined output (stdout and stderr)
		cmd := exec.Command("./bin/lokalise2", cmdArgs...)
		outputBytes, err := cmd.CombinedOutput()
		output := string(outputBytes)

		if err == nil {
			// Download succeeded
			fmt.Println("Successfully downloaded files.")
			return nil
		}

		// Check for rate limit error (HTTP status code 429)
		if isRateLimitError(output) {
			// Handle rate limit error with exponential backoff
			if !handleRateLimitError(attempt, currentSleepTime, startTime) {
				return fmt.Errorf("max retry time exceeded; exiting")
			}
			// Increase sleep time exponentially, capped at maxSleepTime
			currentSleepTime = min(currentSleepTime*2, maxSleepTime)
			time.Sleep(time.Duration(currentSleepTime) * time.Second)
			continue // Retry the download
		}

		// Check for "no keys" error (HTTP status code 406)
		if isNoKeysError(output) {
			return fmt.Errorf("no keys for export with current settings; exiting")
		}

		// For any other errors, log the output and continue to the next attempt
		fmt.Printf("Unexpected error during download on attempt %d: %s\n", attempt, output)
	}

	return fmt.Errorf("failed to download files after %d attempts", maxRetries)
}

// handleRateLimitError handles rate limit errors by checking if the total retry time has exceeded the maximum allowed time.
// Returns true if the download should be retried, or false if the max total time has been exceeded.
func handleRateLimitError(attempt, currentSleepTime int, startTime time.Time) bool {
	elapsedTime := time.Since(startTime).Seconds()
	if elapsedTime >= maxTotalTime {
		fmt.Printf("Max retry time exceeded after %d attempts\n", attempt)
		return false
	}
	fmt.Printf("Rate limit error on attempt %d; retrying in %d seconds...\n", attempt, currentSleepTime)
	return true
}

// getEnvAsInt retrieves an environment variable as an integer.
// Returns the default value if the variable is not set or invalid.
func getEnvAsInt(envVar string, defaultVal int) int {
	valStr := os.Getenv(envVar)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil || val < 1 {
		fmt.Printf("Invalid %s; using default of %d\n", envVar, defaultVal)
		return defaultVal
	}
	return val
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
