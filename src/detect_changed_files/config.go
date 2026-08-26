package main

import (
	"fmt"

	"github.com/bodrovis/lokalise-actions-common/v2/fileexts"
	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// Config aggregates inputs parsed from env.
type Config struct {
	FileExts       []string
	FlatNaming     bool
	AlwaysPullBase bool
	BaseLang       string
	Paths          []string
}

// prepareConfig reads action inputs from environment variables, applies
// extension inference when needed, and validates the resulting scope.
func prepareConfig() (Config, error) {
	flatNaming, alwaysPullBase, err := parseBooleanFlags()
	if err != nil {
		return Config{}, err
	}

	paths, err := parsers.ParseRepoRelativePathsEnv("TRANSLATIONS_PATH")
	if err != nil {
		return Config{}, err
	}

	fileExts, err := resolveFileExts()
	if err != nil {
		return Config{}, err
	}

	baseLang, err := parsers.ParseLangEnv("BASE_LANG")
	if err != nil {
		return Config{}, err
	}

	return Config{
		FileExts:       fileExts,
		FlatNaming:     flatNaming,
		AlwaysPullBase: alwaysPullBase,
		BaseLang:       baseLang,
		Paths:          paths,
	}, nil
}

func parseBooleanFlags() (bool, bool, error) {
	flatNaming, err := parsers.ParseBoolEnv("FLAT_NAMING")
	if err != nil {
		return false, false, fmt.Errorf(
			"invalid FLAT_NAMING value: %w",
			err,
		)
	}

	alwaysPullBase, err := parsers.ParseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return false, false, fmt.Errorf(
			"invalid ALWAYS_PULL_BASE value: %w",
			err,
		)
	}

	return flatNaming, alwaysPullBase, nil
}

// resolveFileExts returns normalized file extensions from FILE_EXT or,
// when FILE_EXT is not provided, falls back to FILE_FORMAT.
func resolveFileExts() ([]string, error) {
	return fileexts.ResolveFromEnv("FILE_EXT", "FILE_FORMAT")
}
