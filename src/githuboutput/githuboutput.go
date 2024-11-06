package githuboutput

import (
	"os"
)

// WriteToGitHubOutput writes a key-value pair to the GITHUB_OUTPUT file
// Returns true if successful, false if an error occurred.
func WriteToGitHubOutput(name, value string) bool {
	githubOutput := os.Getenv("GITHUB_OUTPUT")
	if githubOutput == "" {
		return false
	}

	file, err := os.OpenFile(githubOutput, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	defer file.Close()

	_, err = file.WriteString(name + "=" + value + "\n")
	return err == nil
}
