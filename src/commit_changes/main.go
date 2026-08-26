package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
)

// ErrNoChanges is returned when there is nothing staged to commit.
var ErrNoChanges = errors.New("no changes to commit")

// CommandRunner abstracts git invocations for testability.
type CommandRunner interface {
	Run(name string, args ...string) error
	Capture(name string, args ...string) (string, error)
}

type DefaultCommandRunner struct{}

func (DefaultCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func (DefaultCommandRunner) Capture(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

type (
	commitFunc func(CommandRunner) (string, error)
	writeFunc  func(string, string) bool
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	return 0
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
	write writeFunc,
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

		return "", fmt.Errorf(
			"error committing and pushing changes: %w",
			err,
		)
	}

	return branchName, nil
}

func writeOutputs(branchName string, write writeFunc) error {
	if branchName == "" {
		return nil
	}

	if !write("branch_name", branchName) ||
		!write("commit_created", "true") {
		return errors.New("failed to write to GitHub output")
	}

	return nil
}

func splitNonEmptyLines(s string) []string {
	var result []string

	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}

	return result
}

// isExitCode checks whether err has the given exit code.
func isExitCode(err error, code int) bool {
	type exitCoder interface {
		error
		ExitCode() int
	}

	exitErr, ok := errors.AsType[exitCoder](err)
	return ok && exitErr.ExitCode() == code
}

// sanitizeString keeps characters allowed in generated git refs
// and truncates the result to maxLength.
func sanitizeString(input string, maxLength int) string {
	allowed := func(r rune) bool {
		return (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' ||
			r == '-' ||
			r == '/' ||
			r == '.'
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
