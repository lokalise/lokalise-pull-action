package main

import (
	"bytes"
	"fmt"
	"githuboutput"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	ErrNoChanges    = fmt.Errorf("no changes to commit")
	requiredEnvVars = []string{"GITHUB_ACTOR", "GITHUB_SHA", "GITHUB_REF_NAME", "TEMP_BRANCH_PREFIX", "TRANSLATIONS_PATH", "FILE_FORMAT", "BASE_LANG"}
)

func main() {
	err := commitAndPushChanges()
	if err != nil {
		if err == ErrNoChanges {
			fmt.Println("No changes detected, exiting with status 1")
			os.Exit(1)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	}

	if !githuboutput.WriteToGitHubOutput("commit_created", "true") {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output, exiting")
		os.Exit(1)
	}
}

// Commits and pushes changes to GitHub
func commitAndPushChanges() error {
	if err := checkRequiredEnvVars(); err != nil {
		return err
	}

	// Configure git user
	githubActor := os.Getenv("GITHUB_ACTOR")
	userEmail := fmt.Sprintf("%s@users.noreply.github.com", githubActor)
	if err := setGitUser(githubActor, userEmail); err != nil {
		return err
	}

	// Generate branch name
	branchName, err := generateBranchName()
	if err != nil {
		return err
	}

	if !githuboutput.WriteToGitHubOutput("branch_name", branchName) {
		return fmt.Errorf("failed to write branch name to GITHUB_OUTPUT")
	}

	// Checkout the branch
	if err := checkoutBranch(branchName); err != nil {
		return err
	}

	// Prepare and add files for commit
	addArgs := buildGitAddArgs()
	if len(addArgs) == 0 {
		return fmt.Errorf("no files to add, check your configuration")
	}

	if err := runCommand("git", append([]string{"add"}, addArgs...)...); err != nil {
		return fmt.Errorf("failed to add files: %v", err)
	}

	// Commit and push changes
	return commitAndPush(branchName)
}

// Helper Functions

func checkRequiredEnvVars() error {
	for _, key := range requiredEnvVars {
		if os.Getenv(key) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func setGitUser(username, email string) error {
	if err := runCommand("git", "config", "--global", "user.name", username); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runCommand("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}
	fmt.Println("Configured Git user")
	return nil
}

func generateBranchName() (string, error) {
	timestamp := time.Now().Unix()
	shortSHA := os.Getenv("GITHUB_SHA")[:6]
	safeRefName := sanitizeString(os.Getenv("GITHUB_REF_NAME"), 50)
	tempBranchPrefix := os.Getenv("TEMP_BRANCH_PREFIX")
	branchName := fmt.Sprintf("%s_%s_%s_%d", tempBranchPrefix, safeRefName, shortSHA, timestamp)
	return sanitizeString(branchName, 255), nil
}

func checkoutBranch(branchName string) error {
	if err := runCommand("git", "checkout", "-b", branchName); err == nil {
		fmt.Printf("Created and checked out new branch: %s\n", branchName)
		return nil
	}
	// Branch exists; attempting checkout
	if err := runCommand("git", "checkout", branchName); err != nil {
		return fmt.Errorf("failed to checkout branch %s: %v", branchName, err)
	}
	fmt.Printf("Switched to existing branch: %s\n", branchName)
	return nil
}

func buildGitAddArgs() []string {
	translationsPath := parsePaths()
	flatNaming := os.Getenv("FLAT_NAMING") == "true"
	alwaysPullBase := os.Getenv("ALWAYS_PULL_BASE") == "true"
	fileFormat := os.Getenv("FILE_FORMAT")
	baseLang := os.Getenv("BASE_LANG")

	var addArgs []string
	for _, path := range translationsPath {
		if flatNaming {
			addArgs = append(addArgs, fmt.Sprintf("%s/*.%s", path, fileFormat))
			if !alwaysPullBase {
				addArgs = append(addArgs, fmt.Sprintf(":!%s/%s.%s", path, baseLang, fileFormat))
			}
			addArgs = append(addArgs, fmt.Sprintf(":!%s/**/*.%s", path, fileFormat))
		} else {
			addArgs = append(addArgs, fmt.Sprintf("%s/**/*.%s", path, fileFormat))
			if !alwaysPullBase {
				addArgs = append(addArgs, fmt.Sprintf(":!%s/%s/**", path, baseLang))
			}
		}
	}
	return addArgs
}

func commitAndPush(branchName string) error {
	output, err := captureCommandOutput("git", "commit", "-m", "Translations update")
	if err == nil {
		fmt.Println("Commit created, pushing to remote...")
		return runCommand("git", "push", "origin", branchName)
	}
	if strings.Contains(output, "nothing to commit") {
		fmt.Println("No changes to commit")
		return ErrNoChanges
	}
	return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func captureCommandOutput(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func sanitizeString(input string, maxLength int) string {
	var sanitized strings.Builder
	for _, r := range input {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sanitized.WriteRune(r)
		}
	}
	if sanitized.Len() > maxLength {
		return sanitized.String()[:maxLength]
	}
	return sanitized.String()
}

func parsePaths() []string {
	translationsPath := os.Getenv("TRANSLATIONS_PATH")
	var paths []string
	for _, line := range strings.Split(translationsPath, "\n") {
		path := strings.TrimSpace(line)
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
