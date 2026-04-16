package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/v2/normalizers"
	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// Config aggregates inputs parsed from env.
type Config struct {
	FileExt        []string // normalized lowercased extensions without dots (e.g., "json", "strings")
	FlatNaming     bool     // true: locales/en.json; false: locales/en/*.json, locales/fr/*.json
	AlwaysPullBase bool     // if false, base language files/dirs are excluded from change detection
	BaseLang       string   // e.g., "en", "fr_FR"
	Paths          []string // one or more translation roots, e.g., ["locales"]
}

type configInputs struct {
	flatNaming     bool
	alwaysPullBase bool
	paths          []string
	fileExt        []string
	baseLang       string
}

// prepareConfig reads action inputs from environment variables, applies
// extension inference when needed, and validates the resulting scope.
func prepareConfig() (*Config, error) {
	inputs, err := readConfigInputs()
	if err != nil {
		return nil, err
	}

	return buildConfig(inputs), nil
}

func readConfigInputs() (*configInputs, error) {
	flatNaming, alwaysPullBase, err := parseBooleanFlags()
	if err != nil {
		return nil, err
	}

	paths, err := parsers.ParseRepoRelativePathsEnv("TRANSLATIONS_PATH")
	if err != nil {
		return nil, err
	}

	fileExt, err := resolveFileExts()
	if err != nil {
		return nil, err
	}

	baseLang, err := parsers.ParseLangEnv("BASE_LANG")
	if err != nil {
		return nil, err
	}

	return &configInputs{
		flatNaming:     flatNaming,
		alwaysPullBase: alwaysPullBase,
		paths:          paths,
		fileExt:        fileExt,
		baseLang:       baseLang,
	}, nil
}

func buildConfig(inputs *configInputs) *Config {
	return &Config{
		FileExt:        inputs.fileExt,
		FlatNaming:     inputs.flatNaming,
		AlwaysPullBase: inputs.alwaysPullBase,
		BaseLang:       inputs.baseLang,
		Paths:          inputs.paths,
	}
}

func parseBooleanFlags() (flatNaming bool, alwaysPullBase bool, err error) {
	flatNaming, err = parsers.ParseBoolEnv("FLAT_NAMING")
	if err != nil {
		return false, false, fmt.Errorf("invalid FLAT_NAMING value: %v", err)
	}

	alwaysPullBase, err = parsers.ParseBoolEnv("ALWAYS_PULL_BASE")
	if err != nil {
		return false, false, fmt.Errorf("invalid ALWAYS_PULL_BASE value: %v", err)
	}

	return flatNaming, alwaysPullBase, nil
}

// resolveFileExts returns normalized file extensions from FILE_EXT or, if it is
// not provided, falls back to FILE_FORMAT.
func resolveFileExts() ([]string, error) {
	fileExt := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExt) == 0 {
		if inferred := strings.TrimSpace(os.Getenv("FILE_FORMAT")); inferred != "" {
			fileExt = []string{inferred}
		}
	}

	if len(fileExt) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_FORMAT or FILE_EXT environment variables are set")
	}

	return normalizers.NormalizeFileExtensions(fileExt)
}
