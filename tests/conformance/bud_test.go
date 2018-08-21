package conformance

import (
	"path/filepath"
	"os"
	"strings"
	"regexp"

	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Buildah build conformance test", func() {
	var (
		tempdir     string
		err         error
		buildahtest BuildAhTest
	)

	type BuildTest struct {
		Dockerfile        string
		BuildahRegex      string
		DockerRegex       string
		BuildahErrRegex   string
		DockerErrRegex    string
		ExtraOptions      []string
		WithoutDocker     bool
		IsFile            bool
	}

	BeforeEach(func() {
		tempdir, err = CreateTempDirInTempDir()
		if err != nil {
			os.Exit(1)
		}
		buildahtest = BuildahCreate(tempdir)
	})

	AfterEach(func() {
		buildahtest.Cleanup()
		buildahtest.Docker([]string{"rmi", "-f", "buildahimage"})
	})

	DescribeTable("conformance with docker",
		func (test BuildTest) {
			skipKeyWords := []string{"Created", "Id", "RepoTags", "Parent", "Data", "Layers", "Container", "DockerVersion", "VirtualSize", "Size", "Config", "ContainerConfig"}
			dockerfilePath := filepath.Join(buildahtest.TestDataDir, test.Dockerfile)
			dst := buildahtest.TempDir
			buildDir := buildahtest.TempDir
			if test.IsFile {
				dst = filepath.Join(buildahtest.TempDir, "Dockerfile")
			}
			CopyFiles(dockerfilePath, dst)
			tmp := SystemExec("ls", []string{dst})
			tmp.WaitWithDefaultTimeout()

			buildahoptions := []string{"bud", "-t", "buildahimage", buildDir}
			dockeroptions := []string{"build", "-t", "dockerimage", buildDir}
			if len(test.ExtraOptions) != 0 {
				for i := 0; i < len(test.ExtraOptions); i ++ {
					test.ExtraOptions[i] = strings.Replace(test.ExtraOptions[i], "TEMPDIR", buildahtest.TempDir, -1)
				}
				buildahoptions = append([]string{"bud", "-t", "buildahimage"}, append(test.ExtraOptions, buildDir)...)
				dockeroptions = append([]string{"build", "-t", "docker.io/dockerimage"}, append(test.ExtraOptions, buildDir)...)
			}
			buildah := buildahtest.BuildAh(buildahoptions)
			buildah.WaitWithDefaultTimeout()
			Expect(buildah.ExitCode()).To(Equal(0))
			buildahoutput := buildah.OutputToString()
			buildaherr := buildah.ErrorToString()

			push := SystemExec("podman", []string{"push", "buildahimage", "docker-daemon:buildahimage:latest"})
			push.WaitWithDefaultTimeout()
			Expect(push.ExitCode()).To(Equal(0))

			// Commet ID check
			check := buildahtest.Docker([]string{"images", "-f", "reference=buildahimage", "-q"})
			check.WaitWithDefaultTimeout()
			Expect(buildahoutput).To(ContainSubstring(check.OutputToString()))

			// Image info check
			if test.WithoutDocker {
				br := regexp.MustCompile(test.BuildahRegex)
				Expect(br.MatchString(buildahoutput)).To(BeTrue())
				if test.BuildahErrRegex != "" {
					bre := regexp.MustCompile(test.BuildahErrRegex)
					Expect(bre.MatchString(buildaherr)).To(BeTrue())
				}
			} else {
				docker := buildahtest.Docker(dockeroptions)
				docker.WaitWithDefaultTimeout()
				Expect(docker.ExitCode()).To(Equal(0))
				dockeroutput := docker.OutputToString()
				dockererr := docker.ErrorToString()

				if test.BuildahRegex != "" {
					br := regexp.MustCompile(test.BuildahRegex)
					dr := regexp.MustCompile(test.DockerRegex)
					buildahstrs := br.FindAllStringSubmatch(buildahoutput, -1)
					dockerstrs := dr.FindAllStringSubmatch(dockeroutput, -1)
					Expect(len(buildahstrs)).To(Equal(len(dockerstrs)))
					for i := 0; i < len(buildahstrs); i ++ {
						Expect(buildahstrs[i][1]).To(Equal(dockerstrs[i][1]))
					}
				}
				if test.BuildahErrRegex != "" {
					bre := regexp.MustCompile(test.BuildahErrRegex)
					dre := regexp.MustCompile(test.DockerErrRegex)
					Expect(bre.FindStringSubmatch(buildaherr)[1]).To(Equal(dre.FindStringSubmatch(dockererr)[1]))
				}

				buildahimage := buildahtest.Docker([]string{"inspect", "buildahimage:latest"})
				buildahimage.WaitWithDefaultTimeout()
				dockerimage := buildahtest.Docker([]string{"inspect", "dockerimage:latest"})
				dockerimage.WaitWithDefaultTimeout()
				miss, left, diff, same := CompareJSON(dockerimage.OutputToJSON()[0], buildahimage.OutputToJSON()[0], skipKeyWords)
				Expect(same).To(BeTrue(), InspectCompareResult(miss, left, diff))

				//Check fs with container-diff
				fscheck := SystemExec("container-diff", []string{"diff", "daemon://buildahimage", "daemon://dockerimage", "--type=file"})
				fscheck.WaitWithDefaultTimeout()
				fsr := regexp.MustCompile("These entries.*?None")
				Expect(len(fsr.FindAllStringSubmatch(fscheck.OutputToString(), -1))).To(Equal(3))

			}

		},
		Entry("shell test", BuildTest{
			Dockerfile:   "Dockerfile.shell",
			BuildahRegex: "(?s)--> [0-9a-z]+(.*)--",
			DockerRegex:  "(?s)RUN env.*?Running in [0-9a-z]+(.*?)---",
			IsFile:       true,
			ExtraOptions: []string{"--no-cache"},
		}),
		Entry("copy file to root", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_1",
			BuildahRegex:  "[-rw]+.*?/a",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy file to same file", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_2",
			BuildahRegex:  "[-rw]+.*?/a",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy file to workdir", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_3",
			BuildahRegex:  "[-rw]+.*?/b/a",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy file to workdir rename", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_3_1",
			BuildahRegex:  "[-rw]+.*?/b/b",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy folder contents to higher level", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_4",
			BuildahRegex:  "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
			BuildahErrRegex: "/a: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy wildcard folder contents to higher level", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_5",
			BuildahRegex:  "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
			BuildahErrRegex: "(?s)/a: No such file or directory.*?/b/a: No such file or directory.*?/b/b: No such file or director",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy folder with dot contents to higher level", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_6",
			BuildahRegex:  "(?s)[-rw]+.*?/b/1.*?[-rw]+.*?/b/2.*?/b.*?[-rw]+.*?1.*?[-rw]+.*?2",
			BuildahErrRegex: "(?s)/a: No such file or directory.*?/b/a: No such file or directory.*?/b/b: No such file or director",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy root file to different root name", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_7",
			BuildahRegex:  "[-rw]+.*?/a",
			BuildahErrRegex: "/b: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy nested file to different root name", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_8",
			BuildahRegex:  "[-rw]+.*?/a",
			BuildahErrRegex: "/b: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy file to deeper directory with explicit slash", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_9",
			BuildahRegex:  "[-rw]+.*?/a/b/c/1",
			BuildahErrRegex: "/a/b/1: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy file to deeper directory without explicit slash", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_10",
			BuildahRegex:  "[-rw]+.*?/a/b/c",
			BuildahErrRegex: "/a/b/1: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy directory to deeper directory without explicit slash", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_11",
			BuildahRegex:  "[-rw]+.*?/a/b/c/1",
			BuildahErrRegex: "/a/b/1: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy directory to root without explicit slash", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_12",
			BuildahRegex:  "[-rw]+.*?/a/1",
			BuildahErrRegex: "/a/a: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("copy directory trailing to root without explicit slash", BuildTest{
			Dockerfile:    "Dockerfile.copyfrom_13",
			BuildahRegex:  "[-rw]+.*?/a/1",
			BuildahErrRegex: "/a/a: No such file or directory",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("multi stage base", BuildTest{
			Dockerfile:    "Dockerfile.reusebase",
			BuildahRegex:  "[-rw]+.*?/a/1",
			WithoutDocker: true,
			IsFile:        true,
		}),

		Entry("Directory", BuildTest{
			Dockerfile:    "dir",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("copy to dir", BuildTest{
			Dockerfile:    "copy",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("copy dir", BuildTest{
			Dockerfile:    "copydir",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("copy to renamed file", BuildTest{
			Dockerfile:    "copyrename",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("directory with slash", BuildTest{
			Dockerfile:    "overlapdirwithslash",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("directory without slash", BuildTest{
			Dockerfile:    "overlapdirwithoutslash",
			IsFile:        false,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("environment", BuildTest{
			Dockerfile:    "Dockerfile.env",
			IsFile:        true,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("edgecases", BuildTest{
			Dockerfile:    "Dockerfile.edgecases",
			IsFile:        true,
		}),

		Entry("exposed default", BuildTest{
			Dockerfile:    "Dockerfile.exposedefault",
			IsFile:        true,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("add", BuildTest{
			Dockerfile:    "Dockerfile.add",
			IsFile:        true,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("run with JSON", BuildTest{
			Dockerfile:    "Dockerfile.run.args",
			BuildahRegex:  "(first|third|fifth|inner) (second|fourth|sixth|outer)",
			DockerRegex:   "Running in [0-9a-z]+ (first|third|fifth|inner) (second|fourth|sixth|outer)",
			IsFile:        true,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("shell", BuildTest{
			Dockerfile:    "Dockerfile.shell",
			IsFile:        true,
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("wildcard", BuildTest{
			Dockerfile:    "wildcard",
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("volume", BuildTest{
			Dockerfile:    "volume",
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("volumerun", BuildTest{
			Dockerfile:    "volumerun",
			ExtraOptions: []string{"--no-cache"},
		}),

		Entry("mount", BuildTest{
			Dockerfile:    "mount",
			BuildahRegex:  "/tmp/test/file.*?regular file.*?/tmp/test/file2.*?regular file",
			WithoutDocker: true,
			ExtraOptions: []string{"--no-cache", "-v", "TEMPDIR:/tmp/test"},
		}),

		Entry("Transient mount", BuildTest{
			Dockerfile:    "transientmount",
			BuildahRegex:  "file2.*?FROM busybox ENV name value",
			WithoutDocker: true,
			ExtraOptions: []string{"--no-cache", "-v", "TEMPDIR:/mountdir", "-v", "TEMPDIR/Dockerfile.env:/mountfile"},
		}),


	)
})
