package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/githuboutput"

	"github.com/bodrovis/lokalise-actions-common/parsepaths"
)

// This program commits and pushes changes to GitHub if changes were detected.
// It constructs the commit and branch names based on environment variables
// and handles both flat and nested translation file naming conventions.

var (
	ErrNoChanges   = fmt.Errorf("no changes to commit")
	parsePathsFunc = parsepaths.ParsePaths
)

type CommandRunner interface {
	Run(name string, args ...string) error
	Capture(name string, args ...string) (string, error)
}

type DefaultCommandRunner struct{}

func (d DefaultCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (d DefaultCommandRunner) Capture(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// Config holds the environment variables required for the script
type Config struct {
	GitHubActor      string
	GitHubSHA        string
	GitHubRefName    string
	TempBranchPrefix string
	TranslationsPath string
	FileFormat       string
	BaseLang         string
	FlatNaming       bool
	AlwaysPullBase   bool
}

func main() {
	branchName, err := commitAndPushChanges(DefaultCommandRunner{})
	if err != nil {
		if err == ErrNoChanges {
			fmt.Fprintln(os.Stderr, "No changes detected, exiting")
			os.Exit(0) // Exit with code 0 when there are no changes
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	}

	// Indicate that a commit was created
	if !githuboutput.WriteToGitHubOutput("branch_name", branchName) || !githuboutput.WriteToGitHubOutput("commit_created", "true") {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output, exiting")
		os.Exit(1)
	}
}

// commitAndPushChanges commits and pushes changes to GitHub
func commitAndPushChanges(runner CommandRunner) (string, error) {
	config, err := envVarsToConfig()
	if err != nil {
		return "", err
	}

	if err := setGitUser(config, runner); err != nil {
		return "", err
	}

	// Generate a sanitized branch name
	branchName, err := generateBranchName(config)
	if err != nil {
		return "", err
	}

	// Checkout a new branch or switch to it if it already exists
	if err := checkoutBranch(branchName, runner); err != nil {
		return "", err
	}

	// Prepare and add files for commit
	addArgs := buildGitAddArgs(config)
	if len(addArgs) == 0 {
		return "", fmt.Errorf("no files to add, check your configuration")
	}

	// Run 'git add' with the constructed arguments
	if err := runner.Run("git", append([]string{"add"}, addArgs...)...); err != nil {
		return "", fmt.Errorf("failed to add files: %v", err)
	}

	// Commit and push changes
	return branchName, commitAndPush(branchName, runner)
}

// envVarsToConfig constructs a Config object from required environment variables
func envVarsToConfig() (*Config, error) {
	requiredEnvVars := []string{
		"GITHUB_ACTOR",
		"GITHUB_SHA",
		"GITHUB_REF_NAME",
		"TEMP_BRANCH_PREFIX",
		"TRANSLATIONS_PATH",
		"FILE_FORMAT",
		"BASE_LANG",
	}

	requiredEnvBoolVars := []string{
		"FLAT_NAMING",
		"ALWAYS_PULL_BASE",
	}

	envValues := make(map[string]string)
	envBoolValues := make(map[string]bool)

	// Validate and collect required environment variables
	for _, key := range requiredEnvVars {
		value := os.Getenv(key)
		if value == "" {
			return nil, fmt.Errorf("environment variable %s is required", key)
		}
		envValues[key] = value
	}

	for _, key := range requiredEnvBoolVars {
		value, err := parseBoolEnv(key)
		if err != nil {
			return nil, fmt.Errorf("environment variable %s has incorrect value, expected true or false", key)
		}
		envBoolValues[key] = value
	}

	// Construct and return the Config object
	return &Config{
		GitHubActor:      envValues["GITHUB_ACTOR"],
		GitHubSHA:        envValues["GITHUB_SHA"],
		GitHubRefName:    envValues["GITHUB_REF_NAME"],
		TempBranchPrefix: envValues["TEMP_BRANCH_PREFIX"],
		TranslationsPath: envValues["TRANSLATIONS_PATH"],
		FileFormat:       envValues["FILE_FORMAT"],
		BaseLang:         envValues["BASE_LANG"],
		FlatNaming:       envBoolValues["FLAT_NAMING"],
		AlwaysPullBase:   envBoolValues["ALWAYS_PULL_BASE"],
	}, nil
}

// setGitUser configures git user.name and user.email
func setGitUser(config *Config, runner CommandRunner) error {
	email := fmt.Sprintf("%s@users.noreply.github.com", config.GitHubActor)

	if err := runner.Run("git", "config", "--global", "user.name", config.GitHubActor); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runner.Run("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}
	return nil
}

// generateBranchName creates a sanitized branch name based on environment variables
func generateBranchName(config *Config) (string, error) {
	timestamp := time.Now().Unix()
	githubSHA := config.GitHubSHA
	if len(githubSHA) < 6 {
		return "", fmt.Errorf("GITHUB_SHA is too short")
	}
	shortSHA := githubSHA[:6]

	githubRefName := config.GitHubRefName
	safeRefName := sanitizeString(githubRefName, 50)

	tempBranchPrefix := config.TempBranchPrefix
	branchName := fmt.Sprintf("%s_%s_%s_%d", tempBranchPrefix, safeRefName, shortSHA, timestamp)

	return sanitizeString(branchName, 255), nil
}

// checkoutBranch creates and checks out the branch, or switches to it if it already exists
func checkoutBranch(branchName string, runner CommandRunner) error {
	// Try to create a new branch
	if err := runner.Run("git", "checkout", "-b", branchName); err == nil {
		return nil
	}
	// If branch already exists, switch to it
	if err := runner.Run("git", "checkout", branchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %v", branchName, err)
	}
	return nil
}

// buildGitAddArgs constructs the arguments for 'git add' based on the naming convention
func buildGitAddArgs(config *Config) []string {
	translationsPaths := parsePathsFunc(config.TranslationsPath)
	flatNaming := config.FlatNaming
	alwaysPullBase := config.AlwaysPullBase
	fileFormat := config.FileFormat
	baseLang := config.BaseLang

	var addArgs []string
	for _, path := range translationsPaths {
		if flatNaming {
			// Add files matching 'path/*.fileFormat'
			addArgs = append(addArgs, filepath.Join(path, fmt.Sprintf("*.%s", fileFormat)))
			if !alwaysPullBase {
				// Exclude base language file
				addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, fmt.Sprintf("%s.%s", baseLang, fileFormat))))
			}
			// Exclude files in subdirectories
			addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, "**", fmt.Sprintf("*.%s", fileFormat))))
		} else {
			// Add files matching 'path/**/*.fileFormat'
			addArgs = append(addArgs, filepath.Join(path, "**", fmt.Sprintf("*.%s", fileFormat)))
			if !alwaysPullBase {
				// Exclude files under 'path/baseLang/**'
				addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, baseLang, "**")))
			}
		}
	}
	return addArgs
}

func commitAndPush(branchName string, runner CommandRunner) error {
	// Attempt to commit the changes
	output, err := runner.Capture("git", "commit", "-m", "Translations update")
	if err == nil {
		// Commit succeeded, push the branch
		return runner.Run("git", "push", "origin", branchName)
	}
	if strings.Contains(output, "nothing to commit") {
		// No changes to commit
		return ErrNoChanges
	}
	return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
}

// sanitizeString removes unwanted characters from a string and truncates it to maxLength
func sanitizeString(input string, maxLength int) string {
	// Only allow letters, numbers, underscores, and hyphens
	allowed := func(r rune) bool {
		return (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-'
	}

	var sanitized strings.Builder
	for _, r := range input {
		if allowed(r) {
			sanitized.WriteRune(r)
		}
	}

	result := sanitized.String()
	if len(result) > maxLength {
		return result[:maxLength]
	}
	return result
}

// parseBoolEnv parses a boolean environment variable.
// Returns false if the variable is not set or empty.
// Returns an error if the value cannot be parsed as a boolean.
func parseBoolEnv(envVar string) (bool, error) {
	val := os.Getenv(envVar)
	if val == "" {
		return false, nil // Default to false if not set
	}
	return strconv.ParseBool(val)
}
