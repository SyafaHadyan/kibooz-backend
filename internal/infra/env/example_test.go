package env

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// composeOnly are the settings of .env.example that only the compose files read, so no field of Env has them
var composeOnly = map[string]bool{
	"IMAGE_TAG": true, "GOMEMLIMIT": true, "REDIS_MAXMEMORY": true, "API_MEMORY_LIMIT": true, "REDIS_MEMORY_LIMIT": true,
	"ALLOY_MEMORY_LIMIT": true, "COMPOSE_PROFILES": true, "GRAFANA_CLOUD_PROMETHEUS_URL": true, "GRAFANA_CLOUD_PROMETHEUS_USER": true,
	"GRAFANA_CLOUD_LOKI_URL": true, "GRAFANA_CLOUD_LOKI_USER": true, "GRAFANA_CLOUD_TOKEN": true, "GRAFANA_HOST": true,
	"LOG_ARCHIVE_MEMORY_LIMIT": true, "LOG_ARCHIVE_HOST": true,
}

// sampleValues are the required settings, which show a sample value in the example and have no default to compare with
var sampleValues = map[string]bool{"DB_NAME": true, "DB_USERNAME": true, "DB_PASSWORD": true, "JWT_SECRET_KEY": true}

var exampleLine = regexp.MustCompile(`^(#\s*)?([A-Z][A-Z0-9_]+)=(.*)$`)

type exampleSetting struct {
	value     string
	commented bool
}

func readExample(t *testing.T) map[string]exampleSetting {
	t.Helper()

	raw, err := os.ReadFile("../../../.env.example")
	require.NoError(t, err)

	settings := map[string]exampleSetting{}

	for _, line := range strings.Split(string(raw), "\n") {
		match := exampleLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}

		_, twice := settings[match[2]]
		require.False(t, twice, "%s has two lines in .env.example, and the copy of the file would keep only one of them", match[2])

		settings[match[2]] = exampleSetting{value: match[3], commented: match[1] != ""}
	}

	return settings
}

// TestExampleFileListsEverySetting keeps .env.example and Env together. Every setting that the API reads has a line in the
// example, a line that is on shows the default of the setting, and a setting that the API does not read is one that only
// the compose files use. The example is what a new person copies, so a missing or wrong line is a wrong first start.
func TestExampleFileListsEverySetting(t *testing.T) {
	example := readExample(t)
	known := map[string]bool{}
	fields := reflect.TypeFor[Env]()

	for index := range fields.NumField() {
		tag := fields.Field(index).Tag.Get("env")
		if tag == "" {
			continue
		}

		name, _, _ := strings.Cut(tag, ",")
		known[name] = true

		setting, listed := example[name]
		require.True(t, listed, "%s is read by the API and has no line in .env.example, not even a commented one", name)

		// a line with a default shows that default, on or off, and a line that is on without a default stays empty so that copying
		// the file cannot switch a feature on with a made up value. An optional line may show an example value after its #.
		fallback, hasDefault := fields.Field(index).Tag.Lookup("envDefault")

		switch {
		case sampleValues[name]:
		case hasDefault:
			require.Equal(t, fallback, setting.value, "%s has another value in .env.example than its default", name)
		case !setting.commented:
			require.Empty(t, setting.value, "%s has no default in the API, so its line in .env.example must be empty or commented", name)
		}
	}

	for name := range example {
		require.True(t, known[name] || composeOnly[name], "%s is in .env.example, but no field of Env reads it and it is not a compose only setting", name)
	}
}
