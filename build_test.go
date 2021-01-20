package dotnetpublish_test

import (
	"bytes"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	dotnetpublish "github.com/paketo-buildpacks/dotnet-publish"
	"github.com/paketo-buildpacks/dotnet-publish/fakes"
	"github.com/paketo-buildpacks/packit"
	"github.com/paketo-buildpacks/packit/chronos"
	"github.com/paketo-buildpacks/packit/scribe"
	"github.com/sclevine/spec"

	. "github.com/onsi/gomega"
)

func testBuild(t *testing.T, context spec.G, it spec.S) {
	var (
		Expect = NewWithT(t).Expect

		workingDir         string
		rootManager        *fakes.RootManager
		sourceRemover      *fakes.SourceRemover
		publishProcess     *fakes.PublishProcess
		buildpackYMLParser *fakes.BuildpackYMLParser
		build              packit.BuildFunc
		timestamp          time.Time

		buffer *bytes.Buffer
	)

	it.Before(func() {
		var err error
		workingDir, err = ioutil.TempDir("", "working-dir")
		Expect(err).NotTo(HaveOccurred())

		Expect(ioutil.WriteFile(filepath.Join(workingDir, "buildpack.yml"), nil, 0600)).To(Succeed())

		rootManager = &fakes.RootManager{}
		sourceRemover = &fakes.SourceRemover{}
		publishProcess = &fakes.PublishProcess{}

		buildpackYMLParser = &fakes.BuildpackYMLParser{}
		buildpackYMLParser.ParseProjectPathCall.Returns.ProjectFilePath = "some/project/path"

		os.Setenv("DOTNET_ROOT", "some-existing-root-dir")
		os.Setenv("SDK_LOCATION", "some-sdk-location")

		buffer = bytes.NewBuffer(nil)
		logger := scribe.NewLogger(buffer)

		timestamp = time.Now()
		clock := chronos.NewClock(func() time.Time {
			return timestamp
		})

		build = dotnetpublish.Build(rootManager, sourceRemover, publishProcess, buildpackYMLParser, clock, logger)
	})

	it.After(func() {
		os.Unsetenv("DOTNET_ROOT")
		os.Unsetenv("SDK_LOCATION")

		Expect(os.RemoveAll(workingDir)).To(Succeed())
	})

	it("returns a build result", func() {
		result, err := build(packit.BuildContext{
			WorkingDir: workingDir,
			BuildpackInfo: packit.BuildpackInfo{
				Name:    "Some Buildpack",
				Version: "some-version",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(packit.BuildResult{}))

		Expect(rootManager.SetupCall.Receives.Root).To(Equal(filepath.Join(workingDir, ".dotnet-root")))
		Expect(rootManager.SetupCall.Receives.ExistingRoot).To(Equal("some-existing-root-dir"))
		Expect(rootManager.SetupCall.Receives.SdkLocation).To(Equal("some-sdk-location"))

		Expect(sourceRemover.RemoveCall.Receives.WorkingDir).To(Equal(workingDir))
		Expect(sourceRemover.RemoveCall.Receives.PublishOutputDir).To(MatchRegexp(`dotnet-publish-output\d+`))
		Expect(sourceRemover.RemoveCall.Receives.ExcludedFiles).To(ConsistOf([]string{".dotnet-root", ".dotnet_root"}))

		Expect(publishProcess.ExecuteCall.Receives.WorkingDir).To(Equal(workingDir))
		Expect(publishProcess.ExecuteCall.Receives.RootDir).To(Equal(filepath.Join(workingDir, ".dotnet-root")))
		Expect(publishProcess.ExecuteCall.Receives.ProjectPath).To(Equal("some/project/path"))
		Expect(publishProcess.ExecuteCall.Receives.OutputPath).To(MatchRegexp(`dotnet-publish-output\d+`))

		Expect(buffer.String()).To(ContainSubstring("Some Buildpack some-version"))
		Expect(buffer.String()).To(ContainSubstring("Executing build process"))
	})

	context("failure cases", func() {
		context("when the DOTNET_ROOT can not be found", func() {
			it.Before(func() {
				rootManager.SetupCall.Returns.Error = errors.New("some-error")
			})

			it("returns an error", func() {
				_, err := build(packit.BuildContext{
					WorkingDir: workingDir,
				})
				Expect(err).To(MatchError("some-error"))
			})
		})

		context("when the source code cannot be removed", func() {
			it.Before(func() {
				sourceRemover.RemoveCall.Returns.Error = errors.New("some-error")
			})

			it("returns an error", func() {
				_, err := build(packit.BuildContext{
					WorkingDir: workingDir,
				})
				Expect(err).To(MatchError("some-error"))
			})
		})

		context("when the buildpack.yml can not be pased", func() {
			it.Before(func() {
				buildpackYMLParser.ParseProjectPathCall.Returns.Err = errors.New("some-error")
			})
			it("returns an error", func() {
				_, err := build(packit.BuildContext{
					WorkingDir: workingDir,
				})
				Expect(err).To(MatchError("some-error"))
			})
		})

		context("when the publish process fails", func() {
			it.Before(func() {
				publishProcess.ExecuteCall.Returns.Error = errors.New("some-error")
			})

			it("returns an error", func() {
				_, err := build(packit.BuildContext{
					WorkingDir: workingDir,
				})
				Expect(err).To(MatchError("some-error"))
			})
		})
	})
}
