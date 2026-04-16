package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareConfig(t *testing.T) {
	absUnixLike, _ := filepath.Abs("some/abs/path")

	tests := []struct {
		name           string
		envVars        map[string]string
		expectedError  string
		expectedConfig *Config
	}{
		{
			name: "valid config via FILE_FORMAT fallback",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT overrides FILE_FORMAT (single ext)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "structured_json",
				"FILE_EXT":          "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT multi-line (iOS bundle)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          "strings\nstringsdict",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "true",
			},
			expectedConfig: &Config{
				FileExt:        []string{"strings", "stringsdict"},
				FlatNaming:     false,
				AlwaysPullBase: true,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT normalization (leading dot, spaces, casing) and dedupe",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          ".JSON\n json \nJsOn",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "FILE_EXT overrides FILE_FORMAT and is normalized",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "structured_json",
				"FILE_EXT":          ".json\nJSON",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     true,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"path/to/translations"},
			},
		},
		{
			name: "missing TRANSLATIONS_PATH",
			envVars: map[string]string{
				"FILE_FORMAT":      "json",
				"BASE_LANG":        "en",
				"FLAT_NAMING":      "true",
				"ALWAYS_PULL_BASE": "false",
			},
			expectedError: "variable TRANSLATIONS_PATH is required",
		},
		{
			name: "invalid FLAT_NAMING",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "invalid_bool",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "invalid FLAT_NAMING value",
		},
		{
			name: "invalid ALWAYS_PULL_BASE",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "invalid_bool",
			},
			expectedError: "invalid ALWAYS_PULL_BASE value",
		},
		{
			name: "no valid extensions after normalization",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          " \n . \n\t",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "no valid file extensions after normalization",
		},
		{
			name: "cannot infer file extension when FILE_EXT and FILE_FORMAT are missing",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "cannot infer file extension",
		},
		{
			name: "rejects file extension with slash",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          "json/foo",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: `invalid file extension "json/foo"`,
		},
		{
			name: "rejects file extension with backslash",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_EXT":          `json\foo`,
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "invalid file extension \"json\\\\foo\"",
		},
		{
			name: "normalizes relative paths and slashes",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "./a//b/../c",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "true",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: true,
				BaseLang:       "en",
				Paths:          []string{"a/c"},
			},
		},
		{
			name: "rejects parent escape",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "../outside",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "escapes repo root",
		},
		{
			name: "rejects absolute path (platform-agnostic)",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": absUnixLike,
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "path must be relative",
		},
		{
			name: "multiple paths are deduped and normalized",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "./x\nx/\n./x/",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en",
				"FLAT_NAMING":       "false",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedConfig: &Config{
				FileExt:        []string{"json"},
				FlatNaming:     false,
				AlwaysPullBase: false,
				BaseLang:       "en",
				Paths:          []string{"x"},
			},
		},
		{
			name: "missing BASE_LANG",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "BASE_LANG environment variable is not set or empty",
		},
		{
			name: "whitespace-only BASE_LANG",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "   \t   ",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "BASE_LANG environment variable is not set or empty",
		},
		{
			name: "BASE_LANG with slash is rejected",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         "en/us",
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "BASE_LANG must not contain path separators",
		},
		{
			name: "BASE_LANG with backslash is rejected",
			envVars: map[string]string{
				"TRANSLATIONS_PATH": "path/to/translations",
				"FILE_FORMAT":       "json",
				"BASE_LANG":         `en\us`,
				"FLAT_NAMING":       "true",
				"ALWAYS_PULL_BASE":  "false",
			},
			expectedError: "BASE_LANG must not contain path separators",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPrepareConfigEnv(t)

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := prepareConfig()

			if tt.expectedError != "" {
				if err == nil || !containsSubstring(err.Error(), tt.expectedError) {
					t.Fatalf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !reflect.DeepEqual(cfg, tt.expectedConfig) {
				t.Fatalf("expected config %+v, got %+v", tt.expectedConfig, cfg)
			}
		})
	}
}

func clearPrepareConfigEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"TRANSLATIONS_PATH",
		"FILE_FORMAT",
		"FILE_EXT",
		"BASE_LANG",
		"FLAT_NAMING",
		"ALWAYS_PULL_BASE",
	} {
		t.Setenv(key, "")
	}
}

// containsSubstring checks if a string contains a substring.
func containsSubstring(str, substr string) bool {
	return strings.Contains(str, substr)
}
