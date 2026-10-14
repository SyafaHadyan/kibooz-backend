package repocheck_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var goImage = regexp.MustCompile(`golang:[0-9][^\s"]*@sha256:[0-9a-f]{64}`)

// The CI compose file swaps the Go image of the Dockerfile for the copy on the Google mirror by the exact name that the
// Dockerfile uses. When Dependabot moves the Dockerfile to a new digest and nothing moves the compose file, the swap
// matches nothing and the build quietly pulls from Docker Hub again, which is what the swap was there to avoid.
func TestTheCIComposeFileNamesTheSameGoImageAsTheDockerfile(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	require.NoError(t, err)

	compose, err := os.ReadFile("../../compose.ci.yml")
	require.NoError(t, err)

	want := goImage.FindString(string(dockerfile))
	require.NotEmpty(t, want, "the Dockerfile has to name its Go image with a digest")

	named := goImage.FindAllString(string(compose), -1)
	require.NotEmpty(t, named, "compose.ci.yml has to name the Go image")

	for _, image := range named {
		require.Equal(t, want, image, "compose.ci.yml names another Go image than the Dockerfile, so update the digest in both places")
	}

	// once as the name to replace and once as the copy on the mirror
	require.Len(t, named, 2)
}
