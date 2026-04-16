package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/bodrovis/lokalise-actions-common/v2/normalizers"
	"github.com/bodrovis/lokalise-actions-common/v2/parsers"
)

// Config aggregates all inputs required to construct the commit/branch/push.
type Config struct {
	GitHubActor        string   // used for default git user.name and noreply email
	GitHubSHA          string   // used to shorten into branch uniqueness token
	TempBranchPrefix   string   // prefix for generated tmp branches (e.g., "lok")
	FileExt            []string // normalized extensions without dots (e.g., "json", "stringsdict")
	BaseLang           string   // e.g., "en", "fr_FR"
	FlatNaming         bool     // true: locales/en.json ; false: locales/en/app.json
	AlwaysPullBase     bool     // if false, base language files/dir are excluded from the commit
	GitUserName        string   // optional override for git config user.name
	GitUserEmail       string   // optional override for git config user.email
	GitCommitMessage   string   // commit message to use
	GitSignCommits     bool     // add -S for git commit
	OverrideBranchName string   // static branch name to reuse a single PR
	ForcePush          bool     // whether to force-push (overwriting history)
	BaseRef            string   // base branch name (no refs/heads/ prefix)
	HeadRef            string   // PR head branch (when running in a PR), no refs/heads/
	TranslationPaths   []string // one or multiple roots like ["locales"]
}

type translationInputs struct {
	fileExt  []string
	paths    []string
	baseLang string
}

// envVarsToConfig reads env vars, validates required ones, normalizes arrays and returns a Config.
// Notes:
// - FILE_EXT may be a multi-line YAML block; if absent, we fall back to FILE_FORMAT.
// - We strip "refs/heads/" from BaseRef/HeadRef if present.
// - Commit message defaults to "Translations update".
func envVarsToConfig() (*Config, error) {
	requiredStrings, requiredBools, err := readRequiredEnvVars()
	if err != nil {
		return nil, err
	}

	translationInputs, err := readTranslationInputs(requiredStrings["BASE_LANG"])
	if err != nil {
		return nil, err
	}

	return buildConfig(requiredStrings, requiredBools, translationInputs), nil
}

func readRequiredEnvVars() (map[string]string, map[string]bool, error) {
	requiredStrings, err := readRequiredStringEnv(
		"GITHUB_ACTOR",
		"GITHUB_SHA",
		"TEMP_BRANCH_PREFIX",
		"TRANSLATIONS_PATH",
		"BASE_LANG",
	)
	if err != nil {
		return nil, nil, err
	}

	requiredBools, err := readRequiredBoolEnv(
		"FLAT_NAMING",
		"ALWAYS_PULL_BASE",
		"FORCE_PUSH",
	)
	if err != nil {
		return nil, nil, err
	}

	return requiredStrings, requiredBools, nil
}

func readTranslationInputs(baseLangEnv string) (*translationInputs, error) {
	fileExt, err := resolveFileExts()
	if err != nil {
		return nil, err
	}

	paths, err := parsers.ParseRepoRelativePathsEnv("TRANSLATIONS_PATH")
	if err != nil {
		return nil, err
	}

	baseLang, err := parsers.ParseLang("BASE_LANG", baseLangEnv)
	if err != nil {
		return nil, err
	}

	return &translationInputs{
		fileExt:  fileExt,
		paths:    paths,
		baseLang: baseLang,
	}, nil
}

func buildConfig(
	requiredStrings map[string]string,
	requiredBools map[string]bool,
	inputs *translationInputs,
) *Config {
	baseRef, headRef := parseGitRefs()

	return &Config{
		GitHubActor:        requiredStrings["GITHUB_ACTOR"],
		GitHubSHA:          requiredStrings["GITHUB_SHA"],
		TempBranchPrefix:   requiredStrings["TEMP_BRANCH_PREFIX"],
		FileExt:            inputs.fileExt,
		BaseLang:           inputs.baseLang,
		FlatNaming:         requiredBools["FLAT_NAMING"],
		AlwaysPullBase:     requiredBools["ALWAYS_PULL_BASE"],
		GitUserName:        os.Getenv("GIT_USER_NAME"),
		GitUserEmail:       os.Getenv("GIT_USER_EMAIL"),
		GitCommitMessage:   resolveCommitMessage(),
		GitSignCommits:     parseOptionalBoolEnvFalse("GIT_SIGN_COMMITS"),
		OverrideBranchName: os.Getenv("OVERRIDE_BRANCH_NAME"),
		ForcePush:          requiredBools["FORCE_PUSH"],
		BaseRef:            baseRef,
		HeadRef:            headRef,
		TranslationPaths:   inputs.paths,
	}
}

func readRequiredStringEnv(keys ...string) (map[string]string, error) {
	values := make(map[string]string, len(keys))

	for _, key := range keys {
		value := os.Getenv(key)
		if value == "" {
			return nil, fmt.Errorf("environment variable %s is required", key)
		}
		values[key] = value
	}

	return values, nil
}

func readRequiredBoolEnv(keys ...string) (map[string]bool, error) {
	values := make(map[string]bool, len(keys))

	for _, key := range keys {
		value, err := parsers.ParseBoolEnv(key)
		if err != nil {
			return nil, fmt.Errorf("environment variable %s has incorrect value, expected true or false", key)
		}
		values[key] = value
	}

	return values, nil
}

func parseGitRefs() (baseRef, headRef string) {
	baseRef = strings.TrimPrefix(strings.TrimSpace(os.Getenv("BASE_REF")), "refs/heads/")
	headRef = strings.TrimPrefix(strings.TrimSpace(os.Getenv("HEAD_REF")), "refs/heads/")
	return baseRef, headRef
}

// resolveFileExts returns normalized file extensions from FILE_EXT or, if it is
// not provided, falls back to FILE_FORMAT.
func resolveFileExts() ([]string, error) {
	fileExts := parsers.ParseStringArrayEnv("FILE_EXT")
	if len(fileExts) == 0 {
		if inferred := strings.TrimSpace(os.Getenv("FILE_FORMAT")); inferred != "" {
			fileExts = []string{inferred}
		}
	}

	if len(fileExts) == 0 {
		return nil, fmt.Errorf("cannot infer file extension. Make sure FILE_EXT or FILE_FORMAT environment variables are set")
	}

	return normalizers.NormalizeFileExtensions(fileExts)
}

func resolveCommitMessage() string {
	commitMsg := os.Getenv("GIT_COMMIT_MESSAGE")
	if commitMsg == "" {
		return "Translations update"
	}
	return commitMsg
}

// parseOptionalBoolEnvFalse returns false if the variable is unset or invalid.
func parseOptionalBoolEnvFalse(key string) bool {
	value, err := parsers.ParseBoolEnv(key)
	if err != nil {
		return false
	}
	return value
}
