package main

import (
	"fmt"
	"testing"
)

func TestSetGitUser(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "test_actor" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "test_actor@users.noreply.github.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor: "test_actor",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSetGitUser_WithCustomValues(t *testing.T) {
	runner := &MockCommandRunner{
		RunFunc: func(name string, args ...string) error {
			if name == "git" && args[0] == "config" && args[1] == "--global" {
				if args[2] == "user.name" && args[3] == "custom_user" {
					return nil
				}
				if args[2] == "user.email" && args[3] == "custom_email@example.com" {
					return nil
				}
			}
			return fmt.Errorf("unexpected command: %s %v", name, args)
		},
	}

	config := &Config{
		GitHubActor:  "ignored_actor",
		GitUserName:  "custom_user",
		GitUserEmail: "custom_email@example.com",
	}

	err := setGitUser(config, runner)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestResolveGitIdentity(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		wantUserName  string
		wantUserEmail string
	}{
		{
			name: "default actor and noreply email",
			config: &Config{
				GitHubActor: "test_actor",
			},
			wantUserName:  "test_actor",
			wantUserEmail: "test_actor@users.noreply.github.com",
		},
		{
			name: "custom name and custom email",
			config: &Config{
				GitHubActor:  "ignored_actor",
				GitUserName:  "custom_user",
				GitUserEmail: "custom_email@example.com",
			},
			wantUserName:  "custom_user",
			wantUserEmail: "custom_email@example.com",
		},
		{
			name: "custom name only gets custom noreply email",
			config: &Config{
				GitHubActor: "ignored_actor",
				GitUserName: "custom_user",
			},
			wantUserName:  "custom_user",
			wantUserEmail: "custom_user@users.noreply.github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotEmail := resolveGitIdentity(tt.config)

			if gotUser != tt.wantUserName {
				t.Fatalf("username mismatch: got %q want %q", gotUser, tt.wantUserName)
			}
			if gotEmail != tt.wantUserEmail {
				t.Fatalf("email mismatch: got %q want %q", gotEmail, tt.wantUserEmail)
			}
		})
	}
}
