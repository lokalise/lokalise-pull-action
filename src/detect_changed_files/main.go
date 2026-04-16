package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
)

// This program inspects git state to decide whether translation files changed.
// It supports both "flat" layouts (e.g., locales/en.json) and nested layouts
// (e.g., locales/en/app.json), and can optionally exclude base language files.
// Result is written as a GitHub Actions output variable `has_changes`.

// CommandRunner abstracts shell execution for testability (inject a fake runner).
type CommandRunner interface {
	Capture(name string, args ...string) (string, error)
}

type DefaultCommandRunner struct{}

func (d DefaultCommandRunner) Capture(name string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func main() {
	if err := run(); err != nil {
		returnWithError(err.Error())
	}
}

func run() error {
	return runWith(
		prepareConfig,
		detectChangedFiles,
		githuboutput.WriteToGitHubOutput,
		DefaultCommandRunner{},
	)
}

type detectFunc func(*Config, CommandRunner) (bool, error)

func runWith(
	prepare func() (*Config, error),
	detect detectFunc,
	write func(string, string) bool,
	runner CommandRunner,
) error {
	cfg, err := prepare()
	if err != nil {
		return fmt.Errorf("error preparing configuration: %w", err)
	}

	changed, err := detectChanges(cfg, detect, runner)
	if err != nil {
		return err
	}

	if err := writeChangesOutput(changed, write); err != nil {
		return err
	}

	return nil
}

func detectChanges(
	cfg *Config,
	detect detectFunc,
	runner CommandRunner,
) (bool, error) {
	changed, err := detect(cfg, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting changes: %w", err)
	}

	return changed, nil
}

func writeChangesOutput(
	changed bool,
	write func(string, string) bool,
) error {
	outputValue := "false"
	if changed {
		outputValue = "true"
		fmt.Println("Detected changes in translation files.")
	} else {
		fmt.Println("No changes detected in translation files.")
	}

	if !write("has_changes", outputValue) {
		return fmt.Errorf("failed to write to GitHub output")
	}

	return nil
}

func returnWithError(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
