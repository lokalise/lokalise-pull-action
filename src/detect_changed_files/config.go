package main

import (
	"fmt"
	"os"
	"strings"

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

// prepareConfig reads action inputs from environment variables, applies
// extension inference when needed, and validates the resulting scope.
func prepareConfig() (*Config, error) {
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

	baseLang, err := parseBaseLang()
	if err != nil {
		return nil, err
	}

	return &Config{
		FileExt:        fileExt,
		FlatNaming:     flatNaming,
		AlwaysPullBase: alwaysPullBase,
		BaseLang:       baseLang,
		Paths:          paths,
	}, nil
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

	return normalizeFileExts(fileExt)
}

// normalizeFileExts lowercases extensions, removes one leading dot, drops empty
// values, rejects path-like values, and removes duplicates while preserving order.
func normalizeFileExts(fileExt []string) ([]string, error) {
	seen := make(map[string]struct{})
	norm := make([]string, 0, len(fileExt))

	for _, ext := range fileExt {
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

	return norm, nil
}

// parseBaseLang validates the base language identifier used for file or
// directory matching.
func parseBaseLang() (string, error) {
	baseLang := strings.TrimSpace(os.Getenv("BASE_LANG"))
	if baseLang == "" {
		return "", fmt.Errorf("BASE_LANG environment variable is required")
	}
	if strings.ContainsAny(baseLang, `/\`) {
		return "", fmt.Errorf("BASE_LANG must not contain path separators")
	}

	return baseLang, nil
}
