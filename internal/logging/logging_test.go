package logging_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/SyafaHadyan/kibooz-backend/internal/logging"
)

func TestALineIsOneJSONObjectWithTheDate(t *testing.T) {
	var out bytes.Buffer

	logging.New(&out).Info("request", "status", 200)
	logging.New(&out).Debug("hidden")

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n"))
	require.Len(t, lines, 1, "a debug line is below the level")

	var line map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &line))

	require.Equal(t, "INFO", line["level"])
	require.Equal(t, "request", line["msg"])
	require.EqualValues(t, 200, line["status"])

	stamp, ok := line["time"].(string)
	require.True(t, ok)

	parsed, err := time.Parse(time.RFC3339Nano, stamp)
	require.NoError(t, err, "the time has the date and the zone, %q", stamp)
	require.InDelta(t, time.Now().Unix(), parsed.Unix(), 5)
}
