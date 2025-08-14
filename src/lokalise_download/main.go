package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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

// ringBuffer keeps only the last N bytes written (thread-safe).
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func newRingBuffer(n int) *ringBuffer { return &ringBuffer{limit: n} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.limit <= 0 {
		return len(p), nil
	}
	if len(p) >= r.limit {
		// keep only the tail of this chunk
		if cap(r.buf) < r.limit {
			r.buf = make([]byte, 0, r.limit)
		}
		r.buf = append(r.buf[:0], p[len(p)-r.limit:]...)
		return len(p), nil
	}
	need := len(r.buf) + len(p) - r.limit
	if need > 0 {
		r.buf = r.buf[need:]
	}
	r.buf = append(r.buf, p...)
	return len(p), nil
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

func (r *ringBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	// return a copy
	b := make([]byte, len(r.buf))
	copy(b, r.buf)
	return b
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdPath, args...)

	// Combined tail (stdout + stderr) for error parsing & returning to caller.
	rb := newRingBuffer(64 * 1024)

	// Stream to CI logs and mirror into our combined tail.
	cmd.Stdout = io.MultiWriter(os.Stdout, rb)
	cmd.Stderr = io.MultiWriter(os.Stderr, rb)

	err := cmd.Run()

	// Prefer checking the returned error for timeout, but fall back to ctx.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return rb.Bytes(), fmt.Errorf("command timed out after %ds", downloadTimeout)
	}

	// Bubble up process error; keeps the tail in the message like upload does.
	if err != nil {
		var ee *exec.ExitError
		exit := ""
		if errors.As(err, &ee) {
			exit = fmt.Sprintf(" (exit %d)", ee.ExitCode())
		}
		tail := strings.TrimSpace(rb.String())
		if tail != "" {
			return rb.Bytes(), fmt.Errorf("command failed%s: %s: %w", exit, tail, err)
		}
		return rb.Bytes(), fmt.Errorf("command failed%s: %w", exit, err)
	}

	return rb.Bytes(), nil
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
// downloadFiles attempts to download files from Lokalise using the lokalise2 CLI tool.
// It handles rate limiting & transient failures with exponential backoff and clear early exits.
func downloadFiles(config DownloadConfig, downloadExecutor func(cmdPath string, args []string, timeout int) ([]byte, error)) {
	fmt.Println("Starting download from Lokalise")

	args := constructDownloadArgs(config)

	startTime := time.Now()
	sleepTime := config.SleepTime
	maxRetries := config.MaxRetries

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d\n", attempt, maxRetries)

		outputBytes, err := downloadExecutor("./bin/lokalise2", args, config.DownloadTimeout)
		output := string(outputBytes)

		// success path (still fail fast on "no keys")
		if err == nil {
			if isNoKeysError(output) {
				returnWithError("no keys for export with current settings; exiting")
			}
			fmt.Println("Successfully downloaded files.")
			return
		}

		// retryable?
		if isRetryableError(err) {
			// last attempt? bail, don't sleep
			if attempt == maxRetries {
				returnWithError(fmt.Sprintf("Failed to download files after %d attempts. Last error: %v", attempt, err))
			}

			// will we blow the total budget if we sleep?
			elapsed := time.Since(startTime)
			if elapsed+time.Duration(sleepTime)*time.Second >= time.Duration(maxTotalTime)*time.Second {
				returnWithError(fmt.Sprintf("Max retry time exceeded (%d seconds); exiting after %d attempts.", maxTotalTime, attempt))
			}

			fmt.Printf("Retryable error on attempt %d (%v); sleeping %ds before retry...\n", attempt, err, sleepTime)
			time.Sleep(time.Duration(sleepTime) * time.Second)
			sleepTime = min(sleepTime*2, maxSleepTime)
			continue
		}

		// non-retryable: show tail and die
		fmt.Fprintf(os.Stderr, "Unexpected error during download on attempt %d: %s\n", attempt, output)
		returnWithError(fmt.Sprintf("Permanent error during download: %v", err))
	}

	returnWithError(fmt.Sprintf("Failed to download files after %d attempts.", maxRetries))
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if isRateLimitError(err) {
		return true
	}

	msg := strings.ToLower(err.Error())

	// timeouts or polling limit (same set as upload)
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "time exceeded") ||
		strings.Contains(msg, "polling time exceeded limit") {
		return true
	}

	return false
}

func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "api request error 429") ||
		strings.Contains(s, "request error 429") ||
		strings.Contains(s, "rate limit")
}

func isNoKeysError(output string) bool {
	return strings.Contains(strings.ToLower(output), "no keys for export")
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
