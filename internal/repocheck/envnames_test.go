package repocheck_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// an environment variable is capital words joined by underscores, and a digit is allowed inside a word, as in S3 and K6
var envName = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$`)

// Every setting that the API reads has a name in the tags of the Env struct, so a lowercase or mixed name is caught here before
// it reaches an operator who has to type it into a compose file.
func TestEveryEnvironmentVariableOfTheAPIIsInCapitalsWithUnderscores(t *testing.T) {
	source, err := os.ReadFile("../infra/env/env.go")
	require.NoError(t, err)

	tags := regexp.MustCompile(`env:"([^"]+)"`).FindAllStringSubmatch(string(source), -1)
	require.NotEmpty(t, tags, "the Env struct has to name its variables with an env tag")

	for _, tag := range tags {
		require.Regexp(t, envName, tag[1], "the name of the variable %q is not capitals with underscores", tag[1])
	}
}

// The example file is what an operator copies, and a setting that is commented out is still a name that they will uncomment.
func TestEveryNameInTheExampleEnvFileIsInCapitalsWithUnderscores(t *testing.T) {
	source, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)

	names := regexp.MustCompile(`(?m)^#?\s?([A-Za-z][A-Za-z0-9_]*)=`).FindAllStringSubmatch(string(source), -1)
	require.NotEmpty(t, names, ".env.example has to list the settings")

	for _, name := range names {
		require.Regexp(t, envName, name[1], "the name %q in .env.example is not capitals with underscores", name[1])
	}
}
