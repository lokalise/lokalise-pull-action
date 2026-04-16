package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
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

type commitFunc func(CommandRunner) (string, error)

func main() {
	if err := run(); err != nil {
		returnWithError(err.Error())
	}
}

func run() error {
	return runWith(
		commitAndPushChanges,
		githuboutput.WriteToGitHubOutput,
		DefaultCommandRunner{},
	)
}

func runWith(
	commit commitFunc,
	write func(string, string) bool,
	runner CommandRunner,
) error {
	branchName, err := performCommit(commit, runner)
	if err != nil {
		return err
	}

	return writeOutputs(branchName, write)
}

func performCommit(
	commit commitFunc,
	runner CommandRunner,
) (string, error) {
	branchName, err := commit(runner)
	if err != nil {
		if errors.Is(err, ErrNoChanges) {
			fmt.Fprintln(os.Stderr, "No changes detected, exiting")
			return "", nil
		}

		return "", fmt.Errorf("error committing and pushing changes: %w", err)
	}

	return branchName, nil
}

func writeOutputs(
	branchName string,
	write func(string, string) bool,
) error {
	if branchName == "" {
		return nil
	}

	if !write("branch_name", branchName) ||
		!write("commit_created", "true") {
		return fmt.Errorf("failed to write to GitHub output")
	}

	return nil
}

func splitNonEmptyLines(s string) []string {
	var res []string
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		res = append(res, line)
	}
	return res
}

// isExitCode checks whether err has the given exit code.
// Supports both *exec.ExitError and any custom error type implementing ExitCode() int.
func isExitCode(err error, code int) bool {
	type exitCoderError interface {
		error
		ExitCode() int
	}

	if ec, ok := errors.AsType[exitCoderError](err); ok {
		return ec.ExitCode() == code
	}
	return false
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

func returnWithError(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
