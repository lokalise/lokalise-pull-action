package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	upxAvailable = "upx"
	rootSrcDir   = "src"
)

var binaries = []string{
	"lokalise_download",
	"detect_changed_files",
	"commit_changes",
}

func runCommand(cmd string, args []string) error {
	command := exec.Command(cmd, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("error running %s %v: %v", cmd, args, err)
	}
	return nil
}

func lint(path string) {
	fmt.Println("Running go fmt...")
	if err := runCommand("go", []string{"fmt", path}); err != nil {
		fmt.Println(err)
	}

	if err := runCommand("gofumpt", []string{"-l", "-w", path}); err != nil {
		fmt.Println(err)
	}

	fmt.Println("All checks completed!")
}

func main() {
	// Define binDir relative to the project root
	binDir := filepath.Join(getProjectRoot(), "bin")

	// Ensure the bin directory exists
	if err := os.MkdirAll(binDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create bin directory: %v", err)
	}

	if err := runCommand("go", []string{"install", "mvdan.cc/gofumpt@latest"}); err != nil {
		fmt.Println(err)
	}

	for _, binaryName := range binaries {
		fullPkgPath := filepath.Join(getProjectRoot(), rootSrcDir, binaryName)

		err := filepath.Walk(fullPkgPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && filepath.Ext(path) == ".go" {
				lint(path)
			}
			return nil
		})
		if err != nil {
			fmt.Printf("Error walking through %s: %v\n", fullPkgPath, err)
			continue
		}

		// Define output path in the bin directory
		binaryPath := filepath.Join(binDir, binaryName)

		// Build the binary
		if err := buildBinary(fullPkgPath, binaryPath); err != nil {
			log.Printf("Build failed for %s: %v", binaryName, err)
			continue
		}

		// Compress the binary with UPX, if available
		if checkCommand(upxAvailable) {
			if err := compressWithUPX(binaryPath); err != nil {
				log.Printf("Compression failed for %s: %v", binaryName, err)
			}
		} else {
			fmt.Printf("UPX not found; skipping compression for %s.\n", binaryName)
		}

		fmt.Printf("Build complete. Binary located at %s\n", binaryPath)
	}
}

// getProjectRoot returns the absolute path of the project root
func getProjectRoot() string {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get project root: %v", err)
	}
	return root
}

// buildBinary compiles the Go binary
func buildBinary(srcDir, outputPath string) error {
	fmt.Println("Building binary for Linux (Ubuntu)...")
	cmd := exec.Command("go", "build", "-tags=tiny", "-ldflags=-s -w", "-o", outputPath)
	cmd.Dir = srcDir

	// Set environment variables for cross-compilation to Linux
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// compressWithUPX compresses the binary with UPX, if available
func compressWithUPX(binaryPath string) error {
	fmt.Println("Compressing binary with UPX...")
	return runCommand("upx", []string{"--best", "--lzma", binaryPath})
}

// checkCommand checks if a command is available on the system
func checkCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
