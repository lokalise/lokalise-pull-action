package main

import (
	"fmt"
)

// setGitUser ensures git has user.name/user.email configured,
// defaulting to the GitHub actor with a noreply email if not provided by inputs.
func setGitUser(config *Config, runner CommandRunner) error {
	username, email := resolveGitIdentity(config)

	if err := runner.Run("git", "config", "--global", "user.name", username); err != nil {
		return fmt.Errorf("failed to set git user.name: %v", err)
	}
	if err := runner.Run("git", "config", "--global", "user.email", email); err != nil {
		return fmt.Errorf("failed to set git user.email: %v", err)
	}
	return nil
}

func resolveGitIdentity(config *Config) (username, email string) {
	username = config.GitUserName
	email = config.GitUserEmail

	if username == "" {
		username = config.GitHubActor
	}
	if email == "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", username)
	}

	return username, email
}
