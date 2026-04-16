package main

import (
	"context"
	"fmt"
	"os"
)

// exitFunc is a function variable that defaults to os.Exit.
// Overridable in tests to assert exit behavior without actually terminating the process.
// Rationale: makes CLI testable without forking a process.
var exitFunc = os.Exit

type (
	validateFunc func(DownloadConfig) error
	downloadFunc func(context.Context, DownloadConfig, ClientFactory) error
)

func main() {
	if err := run(); err != nil {
		returnWithError(err.Error())
	}
}

func run() error {
	return runWith(
		prepareConfig,
		validateDownloadConfig,
		downloadFiles,
		&LokaliseFactory{},
	)
}

func runWith(
	prepare func() DownloadConfig,
	validate validateFunc,
	download downloadFunc,
	factory ClientFactory,
) error {
	cfg := prepare()

	if err := validate(cfg); err != nil {
		return fmt.Errorf("invalid download config: %w", err)
	}

	// Hard deadline for the whole run to avoid hanging jobs in CI.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DownloadTimeout)
	defer cancel()

	if err := download(ctx, cfg, factory); err != nil {
		return err
	}

	return nil
}

// Kept as a function to allow test substitution via exitFunc.
func returnWithError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	exitFunc(1)
}
