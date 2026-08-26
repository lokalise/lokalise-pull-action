package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
)

// CommandRunner abstracts command execution for testability.
type CommandRunner interface {
	Capture(name string, args ...string) (string, error)
}

type DefaultCommandRunner struct{}

func (DefaultCommandRunner) Capture(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

type (
	prepareFunc func() (Config, error)
	detectFunc  func(Config, CommandRunner) (bool, error)
	writeFunc   func(string, string) bool
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
		prepareConfig,
		detectChangedFiles,
		githuboutput.WriteToGitHubOutput,
		DefaultCommandRunner{},
	)
}

func runWith(
	prepare prepareFunc,
	detect detectFunc,
	write writeFunc,
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

	return writeChangesOutput(changed, write)
}

func detectChanges(
	cfg Config,
	detect detectFunc,
	runner CommandRunner,
) (bool, error) {
	changed, err := detect(cfg, runner)
	if err != nil {
		return false, fmt.Errorf("error detecting changes: %w", err)
	}

	return changed, nil
}

func writeChangesOutput(changed bool, write writeFunc) error {
	outputValue := "false"

	if changed {
		outputValue = "true"
		fmt.Println("Detected changes in translation files.")
	} else {
		fmt.Println("No changes detected in translation files.")
	}

	if !write("has_changes", outputValue) {
		return errors.New("failed to write to GitHub output")
	}

	return nil
}
