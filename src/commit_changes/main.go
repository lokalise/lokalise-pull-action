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
	ErrNoChanges    = fmt.Errorf("no changes to commit")
	requiredEnvVars = []string{
		"GITHUB_ACTOR",
		"GITHUB_SHA",
		"GITHUB_REF_NAME",
		"TEMP_BRANCH_PREFIX",
		"TRANSLATIONS_PATH",
		"FILE_FORMAT",
		"BASE_LANG",
	}
)

func main() {
	if err := commitAndPushChanges(); err != nil {
		if err == ErrNoChanges {
			fmt.Fprintln(os.Stderr, "No changes detected, exiting")
			os.Exit(0) // Exit with code 0 when there are no changes
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	}

	// Indicate that a commit was created
	if !githuboutput.WriteToGitHubOutput("commit_created", "true") {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output, exiting")
		os.Exit(1)
	}
}

// commitAndPushChanges commits and pushes changes to GitHub
func commitAndPushChanges() error {
	// Check that all required environment variables are set
	if err := checkRequiredEnvVars(); err != nil {
		return err
	}

	// Configure git user
	githubActor := os.Getenv("GITHUB_ACTOR")
	userEmail := fmt.Sprintf("%s@users.noreply.github.com", githubActor)
	if err := setGitUser(githubActor, userEmail); err != nil {
		return err
	}

	// Generate a sanitized branch name
	branchName, err := generateBranchName()
	if err != nil {
		return err
	}

	// Write the branch name to GitHub Actions output
	if !githuboutput.WriteToGitHubOutput("branch_name", branchName) {
		return fmt.Errorf("failed to write branch name to GITHUB_OUTPUT")
	}

	// Checkout a new branch or switch to it if it already exists
	if err := checkoutBranch(branchName); err != nil {
		return err
	}

	// Prepare and add files for commit
	addArgs := buildGitAddArgs()
	if len(addArgs) == 0 {
		return fmt.Errorf("no files to add, check your configuration")
	}

	// Run 'git add' with the constructed arguments
	if err := runCommand("git", append([]string{"add"}, addArgs...)...); err != nil {
		return fmt.Errorf("failed to add files: %v", err)
	}

	// Commit and push changes
	return commitAndPush(branchName)
}

// checkRequiredEnvVars ensures that all required environment variables are set
func checkRequiredEnvVars() error {
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			return fmt.Errorf("environment variable %s is required", key)
		}
	}
	return nil
}

// setGitUser configures git user.name and user.email
func setGitUser(username, email string) error {
	if err := runCommand("git", "config", "--global", "user.name", username); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runCommand("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}
	return nil
}

// generateBranchName creates a sanitized branch name based on environment variables
func generateBranchName() (string, error) {
	timestamp := time.Now().Unix()
	githubSHA := os.Getenv("GITHUB_SHA")
	if len(githubSHA) < 6 {
		return "", fmt.Errorf("GITHUB_SHA is too short")
	}
	shortSHA := githubSHA[:6]

	githubRefName := os.Getenv("GITHUB_REF_NAME")
	safeRefName := sanitizeString(githubRefName, 50)

	tempBranchPrefix := os.Getenv("TEMP_BRANCH_PREFIX")
	branchName := fmt.Sprintf("%s_%s_%s_%d", tempBranchPrefix, safeRefName, shortSHA, timestamp)

	return sanitizeString(branchName, 255), nil
}

// checkoutBranch creates and checks out the branch, or switches to it if it already exists
func checkoutBranch(branchName string) error {
	// Try to create a new branch
	if err := runCommand("git", "checkout", "-b", branchName); err == nil {
		return nil
	}
	// If branch already exists, switch to it
	if err := runCommand("git", "checkout", branchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %v", branchName, err)
	}
	return nil
}

// buildGitAddArgs constructs the arguments for 'git add' based on the naming convention
func buildGitAddArgs() []string {
	translationsPaths := parsepaths.ParsePaths(os.Getenv("TRANSLATIONS_PATH"))
	flatNaming, _ := strconv.ParseBool(os.Getenv("FLAT_NAMING"))
	alwaysPullBase, _ := strconv.ParseBool(os.Getenv("ALWAYS_PULL_BASE"))
	fileFormat := os.Getenv("FILE_FORMAT")
	baseLang := os.Getenv("BASE_LANG")

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

// commitAndPush commits the changes and pushes the branch to origin
func commitAndPush(branchName string) error {
	// Attempt to commit the changes
	output, err := captureCommandOutput("git", "commit", "-m", "Translations update")
	if err == nil {
		// Commit succeeded, push the branch
		return runCommand("git", "push", "origin", branchName)
	}
	if strings.Contains(output, "nothing to commit") {
		// No changes to commit
		return ErrNoChanges
	}
	return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
}

// runCommand executes a command and connects stdout and stderr to the respective outputs
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// captureCommandOutput executes a command and captures both stdout and stderr
func captureCommandOutput(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
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
