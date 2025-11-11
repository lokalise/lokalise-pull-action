package githuboutput

import (
	"log"
	"os"
)

// WriteToGitHubOutput writes a key-value pair to the GITHUB_OUTPUT file.
// Returns true if successful, false if an error occurred.
//
// Note: This function assumes that the value does not contain any newlines or special characters
// that need escaping. For more complex values, additional handling is required.
func WriteToGitHubOutput(name, value string) bool {
	githubOutput := os.Getenv("GITHUB_OUTPUT")
	if githubOutput == "" {
		return false // GITHUB_OUTPUT environment variable is not set
	}

	// Open the GITHUB_OUTPUT file in append and write-only mode.
	// The file permissions are ignored here since we're not creating a new file.
	file, err := os.OpenFile(githubOutput, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return false // Unable to open the GITHUB_OUTPUT file
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			log.Printf("Failed to close GITHUB_OUTPUT file (%s): %v", githubOutput, cerr)
		}
	}()

	// Write the key-value pair to the file in the format: name=value
	_, err = file.WriteString(name + "=" + value + "\n")
	return err == nil // Return true if write was successful, false otherwise
}
