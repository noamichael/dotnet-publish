package dotnetpublish

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver"
	"github.com/paketo-buildpacks/packit"
	"github.com/paketo-buildpacks/packit/chronos"
	"github.com/paketo-buildpacks/packit/scribe"
	"github.com/paketo-buildpacks/packit/servicebindings"
)

//go:generate faux --interface SourceRemover --output fakes/source_remover.go
type SourceRemover interface {
	Remove(workingDir, publishOutputDir string, excludedFiles ...string) error
}

//go:generate faux --interface PublishProcess --output fakes/publish_process.go
type PublishProcess interface {
	Execute(workingDir, rootDir, projectPath, outputPath string, flags []string) error
}

//go:generate faux --interface CommandConfigParser --output fakes/command_config_parser.go
type CommandConfigParser interface {
	ParseFlagsFromEnvVar(envVar string) ([]string, error)
}

func Build(
	sourceRemover SourceRemover,
	publishProcess PublishProcess,
	buildpackYMLParser BuildpackYMLParser,
	configParser CommandConfigParser,
	clock chronos.Clock,
	logger scribe.Logger,
) packit.BuildFunc {
	return func(context packit.BuildContext) (packit.BuildResult, error) {
		logger.Title("%s %s", context.BuildpackInfo.Name, context.BuildpackInfo.Version)
		var projectPath string
		var ok bool
		var err error

		if projectPath, ok = os.LookupEnv("BP_DOTNET_PROJECT_PATH"); !ok {
			projectPath, err = buildpackYMLParser.ParseProjectPath(filepath.Join(context.WorkingDir, "buildpack.yml"))
			if err != nil {
				return packit.BuildResult{}, err
			}

			if projectPath != "" {
				nextMajorVersion := semver.MustParse(context.BuildpackInfo.Version).IncMajor()
				logger.Subprocess("WARNING: Setting the project path through buildpack.yml will be deprecated soon in Dotnet Publish Buildpack v%s", nextMajorVersion.String())
				logger.Subprocess("Please specify the project path through the $BP_DOTNET_PROJECT_PATH environment variable instead. See README.md or the documentation on paketo.io for more information.")
			}
		}

		tempDir, err := ioutil.TempDir("", "dotnet-publish-output")
		if err != nil {
			return packit.BuildResult{}, fmt.Errorf("could not create temp directory: %w", err)
		}

		flags, err := configParser.ParseFlagsFromEnvVar("BP_DOTNET_PUBLISH_FLAGS")
		if err != nil {
			return packit.BuildResult{}, err
		}

		// An optional binding that allows users to provide their own NuGet.Config file
		// via a service binding. Since a private registry can be used, it's possible
		// the NuGet.Config contains credentials. Relevent Microsoft docs:
		// https://docs.microsoft.com/en-us/nuget/consume-packages/consuming-packages-authenticated-feeds
		// https://docs.microsoft.com/en-us/nuget/consume-packages/configuring-nuget-behavior#how-settings-are-applied
		serviceBindingResolver := servicebindings.NewResolver()
		nugetConfig, err := serviceBindingResolver.ResolveOne("nuget", "", context.Platform.Path)
		if err == nil {
			logger.Process("Using NuGet.Config binding")
			nugetConfigPath, err := setupNuGetConfig(nugetConfig, context.WorkingDir)
			if err != nil {
				return packit.BuildResult{}, err
			}
			// Do not need to keep this file in the workdir after the publish
			defer os.Remove(nugetConfigPath)
		}

		logger.Process("Executing build process")
		err = publishProcess.Execute(context.WorkingDir, os.Getenv("DOTNET_ROOT"), projectPath, tempDir, flags)
		if err != nil {
			return packit.BuildResult{}, err
		}

		logger.Process("Removing source code")
		logger.Break()
		err = sourceRemover.Remove(context.WorkingDir, tempDir, ".dotnet_root")
		if err != nil {
			return packit.BuildResult{}, err
		}

		err = os.RemoveAll(tempDir)
		if err != nil {
			return packit.BuildResult{}, fmt.Errorf("could not remove temp directory: %w", err)
		}

		return packit.BuildResult{}, nil
	}
}

func setupNuGetConfig(nugetConfig servicebindings.Binding, workingDir string) (string, error) {
	// NOTE: NuGet.Config filename is case-sensitive
	// https://github.com/NuGet/Home/issues/1427
	nugetConfigPath := filepath.Join(nugetConfig.Path, "NuGet.Config")
	// Move the NuGet.Config to the workspace folder
	// Until the dotnet publish and restore are separated,
	// The NuGet.Config MUST exist in the project directory (or above)
	// Once restore is implemented, use -configFile flag
	// see RFC 0003-publish-build-process-config.md
	nugetConfigData, err := ioutil.ReadFile(nugetConfigPath)

	if err != nil {
		return "", err
	}

	workDirNugetConfig := filepath.Join(workingDir, "NuGet.Config")

	err = ioutil.WriteFile(workDirNugetConfig, nugetConfigData, 0644)
	if err != nil {
		return "", err
	}

	return workDirNugetConfig, nil
}
