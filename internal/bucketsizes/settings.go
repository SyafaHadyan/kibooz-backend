package bucketsizes

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Settings is what the exporter reads from its environment
type Settings struct {
	S3       S3Settings
	Targets  []Target
	Interval time.Duration
	// ScanTimeout is how long the listing of one bucket may take
	ScanTimeout time.Duration
	Address     string
}

const (
	defaultInterval = time.Hour
	// the shortest interval, because a listing of a big bucket is hundreds of calls that some stores charge for
	minInterval = time.Minute
	// Address is fixed, because the label kibooz.metrics_port of the compose service names the same port for Alloy
	Address = ":9101"

	defaultScanTimeout = 15 * time.Minute
	minScanTimeout     = time.Minute
)

// LoadSettings reads the settings through getenv, so a test does not have to touch the environment of the process
func LoadSettings(getenv func(string) string) (Settings, error) {
	targets, err := ParseTargets(getenv("BUCKET_SIZES_BUCKET_NAMES"))
	if err != nil {
		return Settings{}, fmt.Errorf("BUCKET_SIZES_BUCKET_NAMES, %w", err)
	}

	settings := Settings{
		S3: S3Settings{
			Endpoint:        strings.TrimSpace(getenv("BUCKET_SIZES_ENDPOINT")),
			Region:          valueOr(getenv("BUCKET_SIZES_REGION"), "auto"),
			AccessKeyID:     strings.TrimSpace(getenv("BUCKET_SIZES_ACCESS_KEY_ID")),
			AccessKeySecret: strings.TrimSpace(getenv("BUCKET_SIZES_ACCESS_KEY_SECRET")),
			PathStyle:       true,
		},
		Targets:     targets,
		Interval:    defaultInterval,
		ScanTimeout: defaultScanTimeout,
		Address:     Address,
	}

	if settings.S3.Endpoint == "" {
		return Settings{}, errors.New("BUCKET_SIZES_ENDPOINT is needed, the address of the store")
	}

	endpoint, err := url.Parse(settings.S3.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" {
		return Settings{}, fmt.Errorf("BUCKET_SIZES_ENDPOINT %q is not an address, write it like https://<account>.r2.cloudflarestorage.com", settings.S3.Endpoint)
	}

	if settings.S3.AccessKeyID == "" || settings.S3.AccessKeySecret == "" {
		return Settings{}, errors.New("BUCKET_SIZES_ACCESS_KEY_ID and BUCKET_SIZES_ACCESS_KEY_SECRET are needed")
	}

	if text := strings.TrimSpace(getenv("BUCKET_SIZES_INTERVAL")); text != "" {
		interval, err := time.ParseDuration(text)
		if err != nil || interval < minInterval {
			return Settings{}, fmt.Errorf("BUCKET_SIZES_INTERVAL %q is not a time of at least %s, write it like 30m or 1h", text, minInterval)
		}

		settings.Interval = interval
	}

	if text := strings.TrimSpace(getenv("BUCKET_SIZES_SCAN_TIMEOUT")); text != "" {
		timeout, err := time.ParseDuration(text)
		if err != nil || timeout < minScanTimeout {
			return Settings{}, fmt.Errorf("BUCKET_SIZES_SCAN_TIMEOUT %q is not a time of at least %s, write it like 30m", text, minScanTimeout)
		}

		settings.ScanTimeout = timeout
	}

	if text := strings.TrimSpace(getenv("BUCKET_SIZES_PATH_STYLE")); text != "" {
		style, err := strconv.ParseBool(text)
		if err != nil {
			return Settings{}, fmt.Errorf("BUCKET_SIZES_PATH_STYLE %q is not true or false", text)
		}

		settings.S3.PathStyle = style
	}

	return settings, nil
}

func valueOr(value string, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}

	return fallback
}

// Port is the port of an address such as :9101 or 127.0.0.1:9101, which the health check of the container calls
func Port(address string) string {
	index := strings.LastIndex(address, ":")
	if index < 0 {
		return ""
	}

	return address[index+1:]
}
