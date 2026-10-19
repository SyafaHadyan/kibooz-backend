package repocheck_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var goImage = regexp.MustCompile(`golang:[0-9][^\s"]*@sha256:[0-9a-f]{64}`)

// The CI compose file swaps the Go image of the Dockerfile for the copy on the Google mirror by the exact name that the
// Dockerfile uses. It takes that name from GO_IMAGE, so Dependabot only has to move the Dockerfile. A copy of the digest in
// the compose file would go stale after the next bump, the swap would match nothing and the build would quietly pull from
// Docker Hub again, which is what the swap was there to avoid.
func TestTheCIComposeFileTakesTheGoImageFromGOIMAGE(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	require.NoError(t, err)

	compose, err := os.ReadFile("../../compose.ci.yml")
	require.NoError(t, err)

	require.NotEmpty(t, goImage.FindString(string(dockerfile)), "the Dockerfile has to name its Go image with a digest")
	require.Empty(t, goImage.FindAllString(string(compose), -1), "compose.ci.yml must not carry a digest of its own, use GO_IMAGE")
	require.Equal(t, 2, strings.Count(string(compose), "${GO_IMAGE"), "GO_IMAGE names the image to replace and the copy on the mirror")
}

// Every workflow that starts the stack with the CI compose file has to put GO_IMAGE in its .env, read from the Dockerfile.
// Without it compose refuses to start, so a workflow that forgot it fails at once and not with a stale digest.
func TestEveryWorkflowThatUsesTheCIComposeFileSetsGOIMAGE(t *testing.T) {
	users := 0

	for _, file := range workflowFiles(t) {
		body, err := os.ReadFile(file)
		require.NoError(t, err)

		if !strings.Contains(string(body), "compose.ci.yml") {
			continue
		}

		users++

		require.Contains(t, string(body), `echo "GO_IMAGE=$(grep -oE 'golang:[0-9][^ "]*@sha256:[0-9a-f]{64}' Dockerfile | head -1)"`,
			"%s uses compose.ci.yml and has to write GO_IMAGE to its .env", file)
	}

	require.NotZero(t, users, "no workflow uses compose.ci.yml, so this test checks nothing")
}

func workflowFiles(t *testing.T) []string {
	t.Helper()

	yaml, err := filepath.Glob("../../.github/workflows/*.yaml")
	require.NoError(t, err)

	yml, err := filepath.Glob("../../.github/workflows/*.yml")
	require.NoError(t, err)

	return append(yaml, yml...)
}

var imageStage = regexp.MustCompile(`\.github/tools/image\.sh ([a-z0-9-]+)`)

// Dependabot does not read a docker run in a workflow, so the images of the lint and scan jobs live in .github/tools/Dockerfile and
// the workflows ask for them by stage name. A digest written into a workflow would go stale without anyone being told, and a stage
// that a workflow names but the file lacks would only fail when that job runs.
func TestTheToolImagesComeFromTheToolsDockerfile(t *testing.T) {
	dockerfile, err := os.ReadFile("../../.github/tools/Dockerfile")
	require.NoError(t, err)

	named := 0

	for _, file := range workflowFiles(t) {
		body, err := os.ReadFile(file)
		require.NoError(t, err)

		require.Empty(t, regexp.MustCompile(`@sha256:[0-9a-f]{64}`).FindAllString(string(body), -1),
			"%s names an image digest, put the image in .github/tools/Dockerfile and use image.sh", file)

		for _, match := range imageStage.FindAllStringSubmatch(string(body), -1) {
			named++

			require.Regexp(t, `(?m)^FROM \S+ AS `+regexp.QuoteMeta(match[1])+`$`, string(dockerfile),
				"%s asks for the stage %s, which .github/tools/Dockerfile does not have", file, match[1])
		}
	}

	require.NotZero(t, named, "no workflow uses image.sh, so this test checks nothing")
}
