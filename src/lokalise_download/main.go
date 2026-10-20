package main

import (
	"context"
	"fmt"
	"os"
)

type (
	validateFunc func(DownloadConfig) error
	downloadFunc func(context.Context, DownloadConfig, ClientFactory) error
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
