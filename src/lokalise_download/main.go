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
	defaultMaxRetries = 5
	defaultSleepTime  = 1
	maxSleepTime      = 60
	maxTotalTime      = 300
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "Usage: lokalise_download <project_id> <token>")
		os.Exit(1)
	}

	projectID := os.Args[1]
	token := os.Args[2]

	if err := downloadFiles(projectID, token); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func downloadFiles(projectID, token string) error {
	if projectID == "" || token == "" {
		return fmt.Errorf("project_id and token are required and cannot be empty")
	}

	fmt.Printf("Starting download for project: %s\n", projectID)
	startTime := time.Now()
	maxRetries := getEnvAsInt("MAX_RETRIES", defaultMaxRetries)
	sleepTime := getEnvAsInt("SLEEP_TIME", defaultSleepTime)
	currentSleepTime := sleepTime

	cliAddParams := os.Getenv("CLI_ADD_PARAMS")
	fileFormat := os.Getenv("FILE_FORMAT")
	githubRefName := os.Getenv("GITHUB_REF_NAME")

	if fileFormat == "" {
		return fmt.Errorf("FILE_FORMAT environment variable is required")
	}
	if githubRefName == "" {
		return fmt.Errorf("GITHUB_REF_NAME is required")
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d\n", attempt, maxRetries)

		cmdArgs := []string{
			"--token=" + token,
			"--project-id=" + projectID,
			"file", "download",
			"--format=" + fileFormat,
			"--original-filenames=true",
			"--directory-prefix=/",
			"--include-tags=" + githubRefName,
		}

		if cliAddParams != "" {
			cmdArgs = append(cmdArgs, strings.Fields(cliAddParams)...)
		}

		cmd := exec.Command("./bin/lokalise2", cmdArgs...)
		outputBytes, err := cmd.CombinedOutput()
		output := string(outputBytes)

		if err == nil {
			fmt.Println("Successfully downloaded files.")
			return nil
		}

		if strings.Contains(output, "API request error 429") {
			if !handleRateLimitError(attempt, currentSleepTime, startTime) {
				return fmt.Errorf("max retry time exceeded; exiting")
			}
			currentSleepTime = min(currentSleepTime*2, maxSleepTime)
			time.Sleep(time.Duration(currentSleepTime) * time.Second)
			continue
		}

		if strings.Contains(output, "API request error 406") {
			return fmt.Errorf("no keys for export with current settings; exiting")
		}

		fmt.Printf("Unexpected error during download on attempt %d: %s\n", attempt, output)
	}

	return fmt.Errorf("failed to download files after %d attempts", maxRetries)
}

func handleRateLimitError(attempt, currentSleepTime int, startTime time.Time) bool {
	elapsedTime := time.Since(startTime).Seconds()
	if elapsedTime >= maxTotalTime {
		fmt.Printf("Max retry time exceeded after %d attempts\n", attempt)
		return false
	}
	fmt.Printf("Rate limit error on attempt %d; retrying in %d seconds...\n", attempt, currentSleepTime)
	return true
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
