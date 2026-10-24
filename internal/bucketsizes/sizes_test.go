package bucketsizes

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	for text, want := range map[string]int64{
		"100": 100, "100B": 100, "1KB": 1000, "500MB": 500_000_000, "10GB": 10_000_000_000, "1.5GB": 1_500_000_000, "2TB": 2_000_000_000_000,
		" 5 gb ": 5_000_000_000, "5gb": 5_000_000_000,
	} {
		got, err := ParseSize(text)
		require.NoError(t, err, text)
		require.Equal(t, want, got, text)
	}

	for _, text := range []string{"", "GB", "ten", "0", "-5GB", "5 parsecs", "5GBGB", "inf", "NaN", "INFB", "10000000000TB", "1e30"} {
		_, err := ParseSize(text)
		require.Error(t, err, text)
	}
}

func TestParseTargets(t *testing.T) {
	targets, err := ParseTargets(" media , chat=5GB,logs=20gb,, ")
	require.NoError(t, err)
	require.Equal(t, []Target{{Bucket: "media"}, {Bucket: "chat", Limit: 5_000_000_000}, {Bucket: "logs", Limit: 20_000_000_000}}, targets)

	for name, spec := range map[string]string{
		"empty": "", "only commas": " , ,", "no name": "=5GB", "twice": "a,a=1GB", "bad limit": "a=lots", "zero limit": "a=0",
	} {
		_, err := ParseTargets(spec)
		require.Error(t, err, name)
	}
}

// fakeLister answers from a table, one answer per bucket, and counts the calls
type fakeLister struct {
	mu      sync.Mutex
	answers map[string]Totals
	failing map[string]error
	waits   map[string]bool
	calls   []string
}

func (f *fakeLister) Totals(ctx context.Context, bucket string) (Totals, error) {
	f.mu.Lock()
	f.calls = append(f.calls, bucket)
	err := f.failing[bucket]
	totals := f.answers[bucket]
	wait := f.waits[bucket]
	f.mu.Unlock()

	if wait {
		<-ctx.Done()

		return Totals{}, ctx.Err()
	}

	return totals, err
}

func newScanner(t *testing.T, lister Lister, targets []Target) (*Scanner, *prometheus.Registry) {
	t.Helper()

	registry := prometheus.NewRegistry()

	return NewScanner(lister, targets, time.Second, registry), registry
}

func TestABucketThatWasNeverMeasuredShowsAFailedScan(t *testing.T) {
	scanner, registry := newScanner(t, &fakeLister{}, []Target{{Bucket: "media", Limit: 5_000_000_000}, {Bucket: "chat"}})

	require.Equal(t, 0.0, testutil.ToFloat64(scanner.success.WithLabelValues("media")))
	require.Equal(t, 0.0, testutil.ToFloat64(scanner.success.WithLabelValues("chat")))
	require.Equal(t, 5_000_000_000.0, testutil.ToFloat64(scanner.limit.WithLabelValues("media")))

	// a bucket without a limit has no series for it, so the alert on the share of the limit does not read a zero
	count, err := testutil.GatherAndCount(registry, "kibooz_bucket_limit_bytes")
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestAScanPublishesTheSizeTheCountAndTheTime(t *testing.T) {
	lister := &fakeLister{answers: map[string]Totals{"media": {Bytes: 1234, Objects: 7}, "chat": {Bytes: 99, Objects: 1}}}
	scanner, _ := newScanner(t, lister, []Target{{Bucket: "media"}, {Bucket: "chat"}})
	scanner.now = func() time.Time { return time.Unix(1_800_000_000, 0) }

	scanner.Scan(context.Background())

	require.Equal(t, 1234.0, testutil.ToFloat64(scanner.size.WithLabelValues("media")))
	require.Equal(t, 7.0, testutil.ToFloat64(scanner.objects.WithLabelValues("media")))
	require.Equal(t, 99.0, testutil.ToFloat64(scanner.size.WithLabelValues("chat")))
	require.Equal(t, 1.0, testutil.ToFloat64(scanner.success.WithLabelValues("media")))
	require.Equal(t, 1_800_000_000.0, testutil.ToFloat64(scanner.scannedAt.WithLabelValues("media")))
	require.Equal(t, []string{"media", "chat"}, lister.calls)
}

func TestAFailedScanKeepsTheLastNumbersAndTheTimeOfTheLastGoodOne(t *testing.T) {
	lister := &fakeLister{answers: map[string]Totals{"media": {Bytes: 500, Objects: 5}, "chat": {Bytes: 10, Objects: 1}}}
	scanner, _ := newScanner(t, lister, []Target{{Bucket: "media"}, {Bucket: "chat"}})
	scanner.now = func() time.Time { return time.Unix(1_800_000_000, 0) }

	scanner.Scan(context.Background())

	scanner.now = func() time.Time { return time.Unix(1_800_003_600, 0) }
	lister.failing = map[string]error{"media": errors.New("403 AccessDenied")}
	lister.answers["chat"] = Totals{Bytes: 20, Objects: 2}

	scanner.Scan(context.Background())

	require.Equal(t, 0.0, testutil.ToFloat64(scanner.success.WithLabelValues("media")), "the failed bucket says so")
	require.Equal(t, 500.0, testutil.ToFloat64(scanner.size.WithLabelValues("media")), "and keeps its last size")
	require.Equal(t, 1_800_000_000.0, testutil.ToFloat64(scanner.scannedAt.WithLabelValues("media")), "and the time of the scan that worked")
	require.Equal(t, 20.0, testutil.ToFloat64(scanner.size.WithLabelValues("chat")), "the other bucket is still measured")
	require.Equal(t, 1.0, testutil.ToFloat64(scanner.success.WithLabelValues("chat")))
}

func TestAScanThatTakesTooLongIsAFailureAndTheNextBucketStillRuns(t *testing.T) {
	lister := &fakeLister{answers: map[string]Totals{"chat": {Bytes: 3, Objects: 1}}, waits: map[string]bool{"media": true}}
	registry := prometheus.NewRegistry()
	scanner := NewScanner(lister, []Target{{Bucket: "media"}, {Bucket: "chat"}}, 20*time.Millisecond, registry)

	scanner.Scan(context.Background())

	require.Equal(t, 0.0, testutil.ToFloat64(scanner.success.WithLabelValues("media")))
	require.Equal(t, 1.0, testutil.ToFloat64(scanner.success.WithLabelValues("chat")))
}

func TestRunScansAtOnceAndThenEveryInterval(t *testing.T) {
	lister := &fakeLister{answers: map[string]Totals{"media": {Bytes: 1, Objects: 1}}}
	scanner, _ := newScanner(t, lister, []Target{{Bucket: "media"}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		scanner.Run(ctx, 5*time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool {
		lister.mu.Lock()
		defer lister.mu.Unlock()

		return len(lister.calls) >= 3
	}, 2*time.Second, 5*time.Millisecond, "one scan at the start and one for each tick")

	cancel()
	<-done
}

func TestTheGaugesCarryTheBucketLabelAndTheHelpOfTheMetric(t *testing.T) {
	lister := &fakeLister{answers: map[string]Totals{"media": {Bytes: 42, Objects: 2}}}
	scanner, registry := newScanner(t, lister, []Target{{Bucket: "media"}})
	scanner.Scan(context.Background())

	families, err := registry.Gather()
	require.NoError(t, err)

	names := []string{}
	for _, family := range families {
		names = append(names, family.GetName())
		require.True(t, strings.HasPrefix(family.GetName(), "kibooz_bucket_"))
		require.Equal(t, "bucket", family.GetMetric()[0].GetLabel()[0].GetName())
	}

	require.ElementsMatch(t, []string{
		"kibooz_bucket_size_bytes", "kibooz_bucket_objects", "kibooz_bucket_scan_success", "kibooz_bucket_scan_timestamp_seconds",
		"kibooz_bucket_scan_duration_seconds",
	}, names)
}
