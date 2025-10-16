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

// This program stages translation files (with inclusion/exclusion rules),
// creates a commit on a temp (or overridden) branch, and pushes it to origin.
// The PR itself is handled by a separate script.
//
// Design goals:
// - Work both on normal push workflows and PR workflows.
// - Respect "flat" vs "nested" i18n layouts.
// - Optionally exclude base language changes (to reduce noisy diffs).
// - Be idempotent over repeated runs with the same override branch.

// ErrNoChanges is returned when there is nothing staged to commit.
var ErrNoChanges = fmt.Errorf("no changes to commit")

// CommandRunner abstracts git invocations for testability.
type CommandRunner interface {
	Run(name string, args ...string) error
	Capture(name string, args ...string) (string, error)
}

// DefaultCommandRunner pipes git stdout/stderr to the current process for visibility.
type DefaultCommandRunner struct{}

func (d DefaultCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Capture returns combined stdout+stderr as a string, useful for parsing or error messages.
func (d DefaultCommandRunner) Capture(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// Config aggregates all inputs required to construct the commit/branch/push.
type Config struct {
	GitHubActor        string   // used for default git user.name and noreply email
	GitHubSHA          string   // used to shorten into branch uniqueness token
	TempBranchPrefix   string   // prefix for generated tmp branches (e.g., "lok")
	FileExt            []string // normalized extensions without dots (e.g., "json", "stringsdict")
	BaseLang           string   // e.g., "en", "fr_FR"
	FlatNaming         bool     // true: locales/en.json ; false: locales/en/app.json
	AlwaysPullBase     bool     // if false, base language files/dir are excluded from the commit
	GitUserName        string   // optional override for git config user.name
	GitUserEmail       string   // optional override for git config user.email
	GitCommitMessage   string   // commit message to use
	OverrideBranchName string   // static branch name to reuse a single PR
	ForcePush          bool     // whether to force-push (overwriting history)
	BaseRef            string   // base branch name (no refs/heads/ prefix)
	HeadRef            string   // PR head branch (when running in a PR), no refs/heads/
	TranslationPaths   []string // one or multiple roots like ["locales"]
}

func main() {
	branchName, err := commitAndPushChanges(DefaultCommandRunner{})
	if err != nil {
		if err == ErrNoChanges {
			// Not an error for CI: just exit 0 to avoid failing the workflow.
			fmt.Fprintln(os.Stderr, "No changes detected, exiting")
			os.Exit(0)
		}

		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	// Tell the composite action what's the branch and that a commit was produced.
	if !githuboutput.WriteToGitHubOutput("branch_name", branchName) ||
		!githuboutput.WriteToGitHubOutput("commit_created", "true") {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output, exiting")
		os.Exit(1)
	}
}

// commitAndPushChanges wires the whole flow: config -> git user -> base ref -> branch -> add -> commit -> push.
func commitAndPushChanges(runner CommandRunner) (string, error) {
	config, err := envVarsToConfig()
	if err != nil {
		return "", err
	}

	// Ensure git user identity is set (otherwise git may refuse to commit in CI).
	if err := setGitUser(config, runner); err != nil {
		return "", err
	}

	// Guard against synthetic refs like "merge" in PR events.
	realBase, err := resolveRealBase(runner, config)
	if err != nil {
		return "", err
	}
	fmt.Printf("Using base branch: %s\n", realBase)

	// Compute a safe branch name. Either static (override) or temp with prefix/ref/sha/timestamp.
	branchName, err := generateBranchName(config)
	if err != nil {
		return "", err
	}

	// Create/switch to the working branch. We try origin/<ref> first to align with remote history.
	if err := checkoutBranch(branchName, realBase, config.HeadRef, runner); err != nil {
		return "", err
	}

	// Build pathspecs for `git add` respecting layout and base-lang policy.
	addArgs := buildGitAddArgs(config)
	if len(addArgs) == 0 {
		return "", fmt.Errorf("no files to add, check your configuration")
	}

	// Stage files (note: we always pass "--" to separate options from pathspecs).
	if err := runner.Run("git", append([]string{"add", "--"}, addArgs...)...); err != nil {
		return "", fmt.Errorf("failed to add files: %v", err)
	}

	// Commit & push (force if requested).
	return branchName, commitAndPush(branchName, runner, config)
}

// envVarsToConfig reads env vars, validates required ones, normalizes arrays and returns a Config.
// Notes:
// - FILE_EXT may be a multi-line YAML block; if absent, we fall back to FILE_FORMAT.
// - We strip "refs/heads/" from BaseRef/HeadRef if present.
// - Commit message defaults to "Translations update".
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

	// Extensions: normalize/dedupe lower-cased, no leading dot.
	fileExts := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExts) == 0 {
		if inferred := os.Getenv("FILE_FORMAT"); inferred != "" {
			fileExts = []string{inferred}
		}
	}
	if len(fileExts) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_EXT or FILE_FORMAT environment variables are set")
	}

	seen := make(map[string]struct{})
	norm := make([]string, 0, len(fileExts))
	for _, ext := range fileExts {
		e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		if e == "" {
			continue
		}
		if strings.ContainsAny(e, `/\`) {
			return nil, fmt.Errorf("invalid file extension %q", ext)
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

	// validate TranslationPaths: repo-relative + ToSlash + dedupe
	paths, err := parsers.ParseRepoRelativePathsEnv("TRANSLATIONS_PATH")
	if err != nil {
		return nil, err
	}

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
		TranslationPaths:   paths,
	}, nil
}

// setGitUser ensures git has user.name/user.email configured,
// defaulting to the GitHub actor with a noreply email if not provided by inputs.
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

// generateBranchName returns either the override branch (sanitized) or a temp branch
// with pattern "<prefix>_<base>_<sha6>_<unixTs>".
// Notes:
// - We keep "/" allowed to support hierarchical branch names (e.g., lok/feature/...).
// - Length is capped to 255 to satisfy git ref constraints.
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

// checkoutBranch bases the working branch off either the PR head (when updating an existing PR)
// or the base branch. We fetch the exact remote ref to work with shallow clones reliably.
func checkoutBranch(branchName, baseRef, headRef string, runner CommandRunner) error {
	// Helper: fetch one remote branch ref without tags and prune stale ones.
	fetch := func(ref string) {
		// "+A:B" syntax forces update of the local remote-tracking ref.
		_, _ = runner.Capture("git", "fetch", "--no-tags", "--prune", "origin",
			fmt.Sprintf("+refs/heads/%[1]s:refs/remotes/origin/%[1]s", ref))
	}

	// Updating an existing PR head? Recreate branch from origin/headRef.
	if headRef != "" && branchName == headRef {
		fetch(headRef)
		if err := runner.Run("git", "checkout", "-B", branchName, "origin/"+headRef); err == nil {
			return nil
		}

		// Fallback to local ref if remote-tracking ref is absent.
		if err := runner.Run("git", "checkout", "-B", branchName, headRef); err == nil {
			return nil
		}

		// Last resort: try a plain checkout (branch must already exist locally).
		return runner.Run("git", "checkout", branchName)
	}

	// Creating/resetting a temp branch based on the base ref.
	fetch(baseRef)
	if err := runner.Run("git", "checkout", "-B", branchName, "origin/"+baseRef); err == nil {
		return nil
	}
	if err := runner.Run("git", "checkout", "-B", branchName, baseRef); err == nil {
		return nil
	}
	return runner.Run("git", "checkout", branchName)
}

// buildGitAddArgs constructs git pathspecs for `git add` that:
// - Include only translation files by extension under given roots;
// - In flat mode, exclude the base language single file (per ext) and any subdirs;
// - In nested mode, exclude the entire base language directory when AlwaysPullBase=false.
//
// We use Git's own globbing (not shell), hence explicit ":"-prefixed excludes (:!) and a final "--".
func buildGitAddArgs(config *Config) []string {
	paths := config.TranslationPaths
	flat := config.FlatNaming
	always := config.AlwaysPullBase
	base := config.BaseLang
	exts := config.FileExt

	var args []string
	for _, p := range paths {
		if flat {
			for _, ext := range exts {
				args = append(args, joinSlash(p, fmt.Sprintf("*.%s", ext)))
				if !always && base != "" {
					args = append(args, ":!"+joinSlash(p, fmt.Sprintf("%s.%s", base, ext)))
				}
				args = append(args, ":!"+joinSlash(p, "**", fmt.Sprintf("*.%s", ext)))
			}
		} else {
			for _, ext := range exts {
				args = append(args, joinSlash(p, "**", fmt.Sprintf("*.%s", ext)))
			}
			if !always && base != "" {
				args = append(args, ":!"+joinSlash(p, base, "**"))
			}
		}
	}
	return args
}

// commitAndPush commits staged changes and pushes the branch (forcing if requested).
// Returns ErrNoChanges when nothing is staged (non-fatal for CI).
func commitAndPush(branchName string, runner CommandRunner, config *Config) error {
	out, err := runner.Capture("git", "diff", "--name-only", "--cached")
	if err != nil {
		return fmt.Errorf("failed to inspect staged changes: %v\nOutput: %s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return ErrNoChanges
	}

	output, err := runner.Capture("git", "commit", "-m", config.GitCommitMessage)
	if err == nil {
		if config.ForcePush {
			return runner.Run("git", "push", "--force", "origin", branchName)
		}
		return runner.Run("git", "push", "origin", branchName)
	}

	return fmt.Errorf("failed to commit changes: %v\nOutput: %s", err, output)
}

// sanitizeString whitelists characters acceptable for git refs and trims to maxLength.
// Allowed: letters, digits, underscore, hyphen, slash, dot.
// Notes: We intentionally allow "/" to keep hierarchical branch names.
func sanitizeString(input string, maxLength int) string {
	allowed := func(r rune) bool {
		return (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' || r == '-' ||
			r == '/' || r == '.'
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

// resolveRealBase determines a usable base branch.
// If cfg.BaseRef is empty/synthetic, we ask the remote what HEAD points to,
// using a locale-agnostic, network-first approach.
//
// Order:
//  1. git ls-remote --symref origin HEAD  -> "ref: refs/heads/<branch> HEAD"
//  2. git symbolic-ref --short refs/remotes/origin/HEAD -> "origin/<branch>"
//  3. git remote show origin  -> parse "HEAD branch: <branch>" (best-effort)
//  4. fallback "main"
func resolveRealBase(runner CommandRunner, cfg *Config) (string, error) {
	base := strings.TrimSpace(cfg.BaseRef)
	if !isSyntheticRef(base) {
		return base, nil
	}

	// 1) Ask the remote directly (locale-proof, no local refs needed).
	if br, ok := getDefaultBranchFromLsRemote(runner); ok {
		fmt.Printf("BASE_REF synthetic/empty, using remote HEAD via ls-remote: %s\n", br)
		return br, nil
	}

	// 2) Use local symbolic ref if present (works after a fetch).
	if br, ok := getDefaultBranchFromSymbolicRef(runner); ok {
		fmt.Printf("BASE_REF synthetic/empty, using origin/HEAD via symbolic-ref: %s\n", br)
		return br, nil
	}

	// 3) Best-effort legacy parse (English-only output).
	if br, ok := getDefaultBranchFromRemoteShow(runner); ok {
		fmt.Printf("BASE_REF synthetic/empty, using remote show origin: %s\n", br)
		return br, nil
	}

	// 4) Last resort.
	return "main", nil
}

func getDefaultBranchFromLsRemote(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "ls-remote", "--symref", "origin", "HEAD")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}
	const (
		linePrefix = "ref: "
		lineSuffix = "\tHEAD"
		refPrefix  = "refs/heads/"
	)

	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		// tolerate CRLF
		line = strings.TrimSuffix(line, "\r")

		if !strings.HasPrefix(line, linePrefix) || !strings.HasSuffix(line, lineSuffix) {
			continue
		}

		// strip "ref: " and trailing "\tHEAD"
		if rest, ok := strings.CutPrefix(line, linePrefix); ok {
			ref := strings.TrimSuffix(rest, lineSuffix)
			ref = strings.TrimSpace(ref) // just in case

			// expect "refs/heads/<branch>"
			if br, ok := strings.CutPrefix(ref, refPrefix); ok && br != "" {
				return br, true
			}
		}
	}
	// ignore scanner error: even if long line truncates, our target line is tiny
	return "", false
}

func getDefaultBranchFromSymbolicRef(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(out) // e.g. "origin/main"
	if line == "" {
		return "", false
	}
	if i := strings.Index(line, "/"); i >= 0 && i+1 < len(line) {
		return line[i+1:], true
	}
	return line, true
}

func getDefaultBranchFromRemoteShow(runner CommandRunner) (string, bool) {
	out, err := runner.Capture("git", "remote", "show", "origin")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", false
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if def, ok := strings.CutPrefix(line, "HEAD branch: "); ok {
			def = strings.TrimSpace(def)
			if def != "" {
				return def, true
			}
		}
	}
	return "", false
}

// isSyntheticRef flags CI-provided pseudo-refs we should not base from directly.
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

func joinSlash(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}
