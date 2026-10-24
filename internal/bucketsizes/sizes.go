// Package bucketsizes measures how big the buckets of an S3 compatible store are and publishes the numbers as Prometheus metrics, so
// a dashboard can show the growth of every bucket and an alert can fire before one reaches its limit. It lists the objects of each
// bucket now and then, because no S3 call returns the size of a bucket, and it keeps the last numbers, so a scrape never lists anything.
package bucketsizes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Totals is what the objects of one bucket add up to
type Totals struct {
	Bytes   int64
	Objects int64
}

// Lister adds up the objects of a bucket
type Lister interface {
	Totals(ctx context.Context, bucket string) (Totals, error)
}

// Target is a bucket to measure and the size at which it counts as full, zero when it has no limit
type Target struct {
	Bucket string
	Limit  int64
}

// units are the sizes in bytes. They are the decimal ones, because the free tiers and the limits of the stores are written that way,
// 10 GB of Cloudflare R2 are 10 thousand million bytes.
var units = map[string]int64{
	"": 1, "B": 1, "KB": 1000, "MB": 1000 * 1000, "GB": 1000 * 1000 * 1000, "TB": 1000 * 1000 * 1000 * 1000,
}

// ParseSize reads a size such as 500MB or 10GB, and a plain number counts as bytes
func ParseSize(text string) (int64, error) {
	text = strings.ToUpper(strings.TrimSpace(text))

	digits := strings.TrimRight(text, "BKMGT ")
	unit := strings.TrimSpace(strings.TrimPrefix(text, digits))

	factor, known := units[unit]
	if !known || digits == "" {
		return 0, fmt.Errorf("%q is not a size, write it like 500MB or 10GB", text)
	}

	number, err := strconv.ParseFloat(strings.TrimSpace(digits), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
		return 0, fmt.Errorf("%q is not a size above zero, write it like 500MB or 10GB", text)
	}

	// about 9 million TB is the most that fits in an int64 of bytes, and no limit of a bucket is near that
	bytes := number * float64(factor)
	if bytes >= math.MaxInt64 {
		return 0, fmt.Errorf("%q is too large", text)
	}

	// a size below one byte would round to zero, which means no limit
	if bytes < 1 {
		return 0, fmt.Errorf("%q is less than a byte", text)
	}

	return int64(bytes), nil
}

// ParseTargets reads a list of buckets separated by commas, each with an optional limit after an equals sign, as in
// "media,chat=5GB,logs=20GB". A bucket that is named twice is refused, because its numbers would overwrite each other.
func ParseTargets(spec string) ([]Target, error) {
	var targets []Target

	seen := map[string]bool{}

	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		name, limit, hasLimit := strings.Cut(item, "=")
		name = strings.TrimSpace(name)

		if name == "" {
			return nil, fmt.Errorf("%q has no bucket name", item)
		}

		if seen[name] {
			return nil, fmt.Errorf("the bucket %q is named twice", name)
		}

		seen[name] = true

		target := Target{Bucket: name}

		if hasLimit {
			size, err := ParseSize(limit)
			if err != nil {
				return nil, fmt.Errorf("the limit of %s, %w", name, err)
			}

			target.Limit = size
		}

		targets = append(targets, target)
	}

	if len(targets) == 0 {
		return nil, errors.New("no bucket is named")
	}

	return targets, nil
}

// Scanner measures the targets and keeps the last result of each in gauges
type Scanner struct {
	lister  Lister
	targets []Target
	now     func() time.Time
	timeout time.Duration

	mu sync.Mutex

	size      *prometheus.GaugeVec
	objects   *prometheus.GaugeVec
	limit     *prometheus.GaugeVec
	success   *prometheus.GaugeVec
	scannedAt *prometheus.GaugeVec
	duration  *prometheus.GaugeVec
}

// NewScanner registers the gauges of the targets with the registry. Every bucket starts as a scan that did not succeed, so a bucket
// that was never measured fires the alert for a failing scan and is not simply missing from the metrics.
func NewScanner(lister Lister, targets []Target, timeout time.Duration, registry prometheus.Registerer) *Scanner {
	gauge := func(name string, help string) *prometheus.GaugeVec {
		vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "kibooz", Subsystem: "bucket", Name: name, Help: help}, []string{"bucket"})
		registry.MustRegister(vec)

		return vec
	}

	scanner := &Scanner{
		lister:    lister,
		targets:   targets,
		now:       time.Now,
		timeout:   timeout,
		size:      gauge("size_bytes", "The size of all the objects of the bucket at the last scan that worked."),
		objects:   gauge("objects", "The number of objects of the bucket at the last scan that worked."),
		limit:     gauge("limit_bytes", "The size at which the bucket counts as full, set for the buckets that have a limit."),
		success:   gauge("scan_success", "1 when the last scan of the bucket worked and 0 when it failed or has not run yet."),
		scannedAt: gauge("scan_timestamp_seconds", "When the last scan of the bucket that worked ended, in seconds since 1970."),
		duration:  gauge("scan_duration_seconds", "How long the last scan of the bucket took, whether it worked or not."),
	}

	for _, target := range targets {
		scanner.success.WithLabelValues(target.Bucket).Set(0)

		if target.Limit > 0 {
			scanner.limit.WithLabelValues(target.Bucket).Set(float64(target.Limit))
		}
	}

	return scanner
}

// Scan measures every target once. A bucket that fails keeps its last numbers and shows scan_success 0, and the next bucket is still measured.
func (s *Scanner) Scan(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, target := range s.targets {
		s.scanOne(ctx, target)
	}
}

func (s *Scanner) scanOne(parent context.Context, target Target) {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()

	begin := s.now()
	totals, err := s.lister.Totals(ctx, target.Bucket)
	elapsed := s.now().Sub(begin)

	s.duration.WithLabelValues(target.Bucket).Set(elapsed.Seconds())

	if err != nil {
		// a stop in the middle of a scan fails every bucket that is left, and the process is on its way out
		if parent.Err() != nil {
			return
		}

		s.success.WithLabelValues(target.Bucket).Set(0)
		log.Printf("the scan of the bucket %s failed %v", target.Bucket, err)

		return
	}

	s.size.WithLabelValues(target.Bucket).Set(float64(totals.Bytes))
	s.objects.WithLabelValues(target.Bucket).Set(float64(totals.Objects))
	s.success.WithLabelValues(target.Bucket).Set(1)
	s.scannedAt.WithLabelValues(target.Bucket).Set(float64(s.now().Unix()))
}

// Run scans at once and then every interval until the context ends
func (s *Scanner) Run(ctx context.Context, interval time.Duration) {
	s.Scan(ctx)

	// the interval is the pause after a scan and not a fixed schedule, so a listing that takes longer than the interval is not followed by
	// another one at once
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.Scan(ctx)
			timer.Reset(interval)
		}
	}
}
