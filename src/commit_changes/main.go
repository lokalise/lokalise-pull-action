package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"

	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// This program commits and pushes changes to GitHub if changes were detected.
// It constructs the commit and branch names based on environment variables
// and handles both flat and nested translation file naming conventions.

var ErrNoChanges = fmt.Errorf("no changes to commit")

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
	GitHubActor        string
	GitHubSHA          string
	TempBranchPrefix   string
	FileExt            []string
	BaseLang           string
	FlatNaming         bool
	AlwaysPullBase     bool
	GitUserName        string
	GitUserEmail       string
	GitCommitMessage   string
	OverrideBranchName string
	ForcePush          bool
	BaseRef            string
	HeadRef            string
}

func main() {
	branchName, err := commitAndPushChanges(DefaultCommandRunner{})
	if err != nil {
		if err == ErrNoChanges {
			fmt.Fprintln(os.Stderr, "No changes detected, exiting")
			os.Exit(0)
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

	realBase, err := resolveRealBase(runner, config)
	if err != nil {
		return "", err
	}
	fmt.Printf("Using base branch: %s\n", realBase)

	// Generate a sanitized branch name
	branchName, err := generateBranchName(config)
	if err != nil {
		return "", err
	}

	// Checkout a new branch or switch to it if it already exists
	if err := checkoutBranch(branchName, realBase, config.HeadRef, runner); err != nil {
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
	return branchName, commitAndPush(branchName, runner, config)
}

// envVarsToConfig constructs a Config object from required environment variables
func envVarsToConfig() (*Config, error) {
	requiredEnvVars := []string{
		"GITHUB_ACTOR",
		"GITHUB_SHA",
		"TEMP_BRANCH_PREFIX",
		"TRANSLATIONS_PATH",
		"BASE_LANG",
	}

	requiredEnvBoolVars := []string{
		"FLAT_NAMING",
		"ALWAYS_PULL_BASE",
		"FORCE_PUSH",
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
		value, err := parsers.ParseBoolEnv(key)
		if err != nil {
			return nil, fmt.Errorf("environment variable %s has incorrect value, expected true or false", key)
		}
		envBoolValues[key] = value
	}

	baseRef := strings.TrimPrefix(strings.TrimSpace(os.Getenv("BASE_REF")), "refs/heads/")
	headRef := strings.TrimPrefix(strings.TrimSpace(os.Getenv("HEAD_REF")), "refs/heads/")

	fileExts := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExts) == 0 {
		if inferred := os.Getenv("FILE_FORMAT"); inferred != "" {
			fileExts = []string{inferred}
		}
	}
	if len(fileExts) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_EXT or FILE_FORMAT environment variables are set")
	}

	// normalize + dedup here
	seen := make(map[string]struct{})
	norm := make([]string, 0, len(fileExts))
	for _, ext := range fileExts {
		e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		norm = append(norm, e)
	}
	if len(norm) == 0 {
		return nil, fmt.Errorf("no valid file extensions after normalization")
	}

	commitMsg := os.Getenv("GIT_COMMIT_MESSAGE")
	if commitMsg == "" {
		commitMsg = "Translations update"
	}

	// Construct and return the Config object
	return &Config{
		GitHubActor:        envValues["GITHUB_ACTOR"],
		GitHubSHA:          envValues["GITHUB_SHA"],
		TempBranchPrefix:   envValues["TEMP_BRANCH_PREFIX"],
		FileExt:            norm,
		BaseLang:           envValues["BASE_LANG"],
		FlatNaming:         envBoolValues["FLAT_NAMING"],
		AlwaysPullBase:     envBoolValues["ALWAYS_PULL_BASE"],
		GitUserName:        os.Getenv("GIT_USER_NAME"),
		GitUserEmail:       os.Getenv("GIT_USER_EMAIL"),
		GitCommitMessage:   commitMsg,
		OverrideBranchName: os.Getenv("OVERRIDE_BRANCH_NAME"),
		ForcePush:          envBoolValues["FORCE_PUSH"],
		BaseRef:            baseRef,
		HeadRef:            headRef,
	}, nil
}

// setGitUser configures git user.name and user.email
func setGitUser(config *Config, runner CommandRunner) error {
	username := config.GitUserName
	email := config.GitUserEmail

	if username == "" {
		username = config.GitHubActor
	}
	if email == "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", username)
	}

	if err := runner.Run("git", "config", "--global", "user.name", username); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runner.Run("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}

	return nil
}

// generateBranchName creates a sanitized branch name based on environment variables
func generateBranchName(config *Config) (string, error) {
	if config.OverrideBranchName != "" {
		return sanitizeString(config.OverrideBranchName, 255), nil
	}

	timestamp := time.Now().Unix()
	githubSHA := config.GitHubSHA
	if len(githubSHA) < 6 {
		return "", fmt.Errorf("GITHUB_SHA is too short")
	}
	shortSHA := githubSHA[:6]

	githubRefName := config.BaseRef
	safeRefName := sanitizeString(githubRefName, 50)

	tempBranchPrefix := config.TempBranchPrefix
	branchName := fmt.Sprintf("%s_%s_%s_%d", tempBranchPrefix, safeRefName, shortSHA, timestamp)

	return sanitizeString(branchName, 255), nil
}

// checkoutBranch creates and checks out the branch, or switches to it if it already exists.
// If branchName == headRef (PR update), base off origin/headRef.
// Otherwise, create/reset from origin/baseRef.
func checkoutBranch(branchName, baseRef, headRef string, runner CommandRunner) error {
	// helper to fetch a single remote branch robustly (works with shallow clones)
	fetch := func(ref string) {
		// +refs/heads/<ref>:refs/remotes/origin/<ref> guarantees we fetch exactly what we need
		_, _ = runner.Capture("git", "fetch", "--no-tags", "--prune", "origin",
			fmt.Sprintf("+refs/heads/%[1]s:refs/remotes/origin/%[1]s", ref))
	}

	// updating existing PR head?
	if headRef != "" && branchName == headRef {
		fetch(headRef)
		if err := runner.Run("git", "checkout", "-B", branchName, "origin/"+headRef); err == nil {
			return nil
		}
		if err := runner.Run("git", "checkout", "-B", branchName, headRef); err == nil {
			return nil
		}
		return runner.Run("git", "checkout", branchName)
	}

	// creating/resetting temp branch off base
	fetch(baseRef)
	if err := runner.Run("git", "checkout", "-B", branchName, "origin/"+baseRef); err == nil {
		return nil
	}
	if err := runner.Run("git", "checkout", "-B", branchName, baseRef); err == nil {
		return nil
	}
	return runner.Run("git", "checkout", branchName)
}

// buildGitAddArgs constructs the arguments for 'git add' based on the naming convention
func buildGitAddArgs(config *Config) []string {
	translationsPaths := parsers.ParseStringArrayEnv("TRANSLATIONS_PATH")
	flatNaming := config.FlatNaming
	alwaysPullBase := config.AlwaysPullBase
	baseLang := config.BaseLang
	fileExts := config.FileExt

	var addArgs []string
	for _, path := range translationsPaths {
		if flatNaming {
			// top-level only: path/*.ext (+ per-ext baseLang exclude, + exclude subdirs)
			for _, ext := range fileExts {
				addArgs = append(addArgs, filepath.Join(path, fmt.Sprintf("*.%s", ext)))
				if !alwaysPullBase && baseLang != "" {
					addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, fmt.Sprintf("%s.%s", baseLang, ext))))
				}
				addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, "**", fmt.Sprintf("*.%s", ext))))
			}
		} else {
			// nested: path/**/*.ext (+ global baseLang dir exclude)
			for _, ext := range fileExts {
				addArgs = append(addArgs, filepath.Join(path, "**", fmt.Sprintf("*.%s", ext)))
			}
			if !alwaysPullBase && baseLang != "" {
				addArgs = append(addArgs, fmt.Sprintf(":!%s", filepath.Join(path, baseLang, "**")))
			}
		}
	}

	return addArgs
}

func commitAndPush(branchName string, runner CommandRunner, config *Config) error {
	// Attempt to commit the changes
	output, err := runner.Capture("git", "commit", "-m", config.GitCommitMessage)
	if err == nil {
		// Commit succeeded, push the branch
		if config.ForcePush {
			return runner.Run("git", "push", "--force", "origin", branchName)
		}
		return runner.Run("git", "push", "origin", branchName)
	}
	if strings.Contains(output, "nothing to commit") {
		return ErrNoChanges
	}
	return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
}

// sanitizeString removes unwanted characters from a string and truncates it to maxLength
func sanitizeString(input string, maxLength int) string {
	// Only allow letters, numbers, underscores, hyphens, and forward slashes
	allowed := func(r rune) bool {
		return (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' ||
			r == '/'
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

func resolveRealBase(runner CommandRunner, cfg *Config) (string, error) {
	base := cfg.BaseRef
	if !isSyntheticRef(base) {
		return base, nil
	}

	// fallback: detect remote default (HEAD) branch
	out, _ := runner.Capture("git", "remote", "show", "origin")
	// look for: "  HEAD branch: main"
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if def, ok := strings.CutPrefix(line, "HEAD branch: "); ok {
			def = strings.TrimSpace(def)
			if def != "" {
				fmt.Printf("BASE_REF synthetic/empty, using remote HEAD: %s\n", def)
				return def, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: scanning remote output failed: %v\n", err)
	}

	// last resort
	return "main", nil
}

func isSyntheticRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "merge" || ref == "head" {
		return true
	}
	if strings.HasPrefix(ref, "refs/pull/") || strings.HasPrefix(ref, "pull/") {
		return true
	}
	if strings.HasSuffix(ref, "/merge") || strings.HasSuffix(ref, "/head") {
		return true
	}
	return false
}
