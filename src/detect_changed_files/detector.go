package main

import "github.com/bodrovis/lokalise-actions-common/v2/managedpaths"

// detectChangedFiles delegates Git path collection and translation-file
// matching to the shared managed-path helpers.
func detectChangedFiles(config Config, runner CommandRunner) (bool, error) {
	return managedpaths.HasManagedGitPaths(
		runner,
		buildTranslationScope(config),
	)
}

func buildTranslationScope(config Config) managedpaths.TranslationScope {
	return managedpaths.TranslationScope{
		Paths:          config.Paths,
		FileExts:       config.FileExts,
		FlatNaming:     config.FlatNaming,
		AlwaysPullBase: config.AlwaysPullBase,
		BaseLang:       config.BaseLang,
	}
}
