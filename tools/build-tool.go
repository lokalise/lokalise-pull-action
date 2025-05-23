package main

import (
	"github.com/bodrovis/lokalise-actions-common/v2/builder"
	"log"
)

const (
	outputDir  = "bin"
	rootSrcDir = "src"
)

var binaries = []string{
	"lokalise_download",
	"detect_changed_files",
	"commit_changes",
}

func main() {
	err := builder.Run(builder.Options{
		SourceRoot: rootSrcDir,
		OutputDir:  outputDir,
		Binaries:   binaries,
		Compress:   true,
		Build:      true,
		Lint:       true,
	})
	if err != nil {
		log.Fatal(err)
	}
}
