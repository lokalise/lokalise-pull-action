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

	// Install gofumpt if needed
	if err := runCommand("go", []string{"install", "mvdan.cc/gofumpt@latest"}); err != nil {
		fmt.Println(err)
	}

	for _, binaryName := range binaries {
		fullPkgPath := filepath.Join(getProjectRoot(), rootSrcDir, binaryName)

		// Lint all .go files in the package
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

		// Build the binaries for all target platforms
		builtBinaries, err := buildBinary(fullPkgPath, binDir, binaryName)
		if err != nil {
			log.Printf("Build failed for %s: %v", binaryName, err)
			continue
		}

		// Compress only Linux binaries with UPX (if available)
		if checkCommand(upxAvailable) {
			for _, binPath := range builtBinaries {
				// Check if it's a Linux binary before compressing
				if isLinuxBinary(binPath) {
					if err := compressWithUPX(binPath); err != nil {
						log.Printf("Compression failed for %s: %v", binPath, err)
					}
				} else {
					fmt.Printf("Skipping UPX compression for macOS binary: %s\n", binPath)
				}
			}
		} else {
			fmt.Println("UPX not found; skipping compression.")
		}

		fmt.Printf("Build complete. Binaries located at %s\n", binDir)
	}
}

func isLinuxBinary(filePath string) bool {
	return filepath.Ext(filePath) == "" && (filepath.Base(filePath) == "linux_amd64" || filepath.Base(filePath) == "linux_arm64")
}

// getProjectRoot returns the absolute path of the project root
func getProjectRoot() string {
	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get project root: %v", err)
	}
	return root
}

// buildBinary compiles the Go binary for multiple platforms and returns the generated file paths
func buildBinary(srcDir, outputDir, binaryName string) ([]string, error) {
	targets := []struct {
		goos   string
		goarch string
		suffix string
	}{
		{"linux", "amd64", "_linux_amd64"},
		{"linux", "arm64", "_linux_arm64"},
		{"darwin", "amd64", "_mac_amd64"},
		{"darwin", "arm64", "_mac_arm64"},
	}

	var builtBinaries []string

	for _, target := range targets {
		outputPath := filepath.Join(outputDir, binaryName+target.suffix)

		fmt.Printf("Building binary for %s/%s -> %s\n", target.goos, target.goarch, outputPath)

		ldflags := "-s -w -extldflags=-static"
		cmd := exec.Command("go", "build", "-tags=netgo,osusergo", "-trimpath", "-ldflags", ldflags, "-o", outputPath)

		cmd.Dir = srcDir
		cmd.Env = append(os.Environ(),
			"GOOS="+target.goos,
			"GOARCH="+target.goarch,
			"CGO_ENABLED=0",
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to build for %s/%s: %w", target.goos, target.goarch, err)
		}

		builtBinaries = append(builtBinaries, outputPath)
	}

	return builtBinaries, nil
}

// compressWithUPX compresses the binary with UPX, if available
func compressWithUPX(binaryPath string) error {
	fmt.Println("Compressing binary with UPX:", binaryPath)
	return runCommand("upx", []string{"--best", "--lzma", binaryPath})
}

// checkCommand checks if a command is available on the system
func checkCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
