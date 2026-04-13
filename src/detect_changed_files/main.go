package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/bodrovis/lokalise-actions-common/v2/githuboutput"
	"github.com/bodrovis/lokalise-actions-common/v2/managedpaths"
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
	config, err := prepareConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error preparing configuration:", err)
		os.Exit(1)
	}

	changed, err := detectChangedFiles(config, DefaultCommandRunner{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error detecting changes:", err)
		os.Exit(1)
	}

	var outputValue string
	if changed {
		outputValue = "true"
		fmt.Println("Detected changes in translation files.")
	} else {
		outputValue = "false"
		fmt.Println("No changes detected in translation files.")
	}

	if !githuboutput.WriteToGitHubOutput("has_changes", outputValue) {
		fmt.Fprintln(os.Stderr, "Failed to write to GitHub output.")
		os.Exit(1)
	}
}

// detectChangedFiles keeps the entrypoint thin by delegating all Git path
// collection and translation-file matching to shared helpers.
func detectChangedFiles(config *Config, runner CommandRunner) (bool, error) {
	scope := buildTranslationScope(config)
	return managedpaths.HasManagedGitPaths(runner, scope)
}

// buildTranslationScope converts env-derived action config into the shared
// managed path scope used by change detection helpers.
func buildTranslationScope(config *Config) managedpaths.TranslationScope {
	return managedpaths.TranslationScope{
		Paths:          config.Paths,
		FileExt:        config.FileExt,
		FlatNaming:     config.FlatNaming,
		AlwaysPullBase: config.AlwaysPullBase,
		BaseLang:       config.BaseLang,
	}
}
