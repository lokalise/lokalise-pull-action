package main

import (
	"github.com/bodrovis/lokalise-actions-common/v2/managedpaths"
)

// detectChangedFiles keeps the entrypoint thin by delegating all Git path
// collection and translation-file matching to shared helpers.
func detectChangedFiles(config *Config, runner CommandRunner) (bool, error) {
	scope := buildTranslationScope(config)
	return managedpaths.HasManagedGitPaths(runner, scope)
}

// buildTranslationScope converts env-derived action config into the shared
// managed path scope used by change detection helpers.
func buildTranslationScope(config *Config) managedpaths.TranslationScope {
	return managedpaths.TranslationScope{
		Paths:          config.Paths,
		FileExt:        config.FileExt,
		FlatNaming:     config.FlatNaming,
		AlwaysPullBase: config.AlwaysPullBase,
		BaseLang:       config.BaseLang,
	}
}
