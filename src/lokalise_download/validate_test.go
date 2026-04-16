package main

import (
	"strings"
	"testing"
)

func TestValidateDownloadConfig_ReturnsError_OnMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  DownloadConfig
		wantErr string
	}{
		{
			name: "missing project id",
			config: DownloadConfig{
				ProjectID:     "",
				Token:         "t",
				FileFormat:    "json",
				GitHubRefName: "ref",
			},
			wantErr: "project ID is required and cannot be empty",
		},
		{
			name: "missing token",
			config: DownloadConfig{
				ProjectID:     "p",
				Token:         "",
				FileFormat:    "json",
				GitHubRefName: "ref",
			},
			wantErr: "API token is required and cannot be empty",
		},
		{
			name: "missing file format",
			config: DownloadConfig{
				ProjectID:     "p",
				Token:         "t",
				FileFormat:    "",
				GitHubRefName: "ref",
			},
			wantErr: "FILE_FORMAT environment variable is required",
		},
		{
			name: "missing github ref when include_tags enabled",
			config: DownloadConfig{
				ProjectID:       "p",
				Token:           "t",
				FileFormat:      "json",
				GitHubRefName:   "",
				SkipIncludeTags: false,
			},
			wantErr: "GITHUB_REF_NAME or GITHUB_HEAD_REF is required when include_tags are enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateDownloadConfig(tt.config)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateDownloadConfig_Success_WhenSkipIncludeTagsEnabled(t *testing.T) {
	t.Parallel()

	err := validateDownloadConfig(DownloadConfig{
		ProjectID:       "p",
		Token:           "t",
		FileFormat:      "json",
		GitHubRefName:   "",
		SkipIncludeTags: true,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateDownloadConfig_Success_WhenAllRequiredFieldsPresent(t *testing.T) {
	t.Parallel()

	err := validateDownloadConfig(DownloadConfig{
		ProjectID:       "p",
		Token:           "t",
		FileFormat:      "json",
		GitHubRefName:   "feature-branch",
		SkipIncludeTags: false,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
