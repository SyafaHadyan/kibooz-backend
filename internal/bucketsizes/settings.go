package bucketsizes

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
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
	// defaultPort is the port of the metrics listener, which BUCKET_SIZES_PORT changes. The compose service sets it and the label
	// kibooz.metrics_port from the same value, so Alloy scrapes the port that the exporter listens on
	defaultPort = "9101"

	defaultScanTimeout = 15 * time.Minute
	minScanTimeout     = time.Minute
)

// LoadSettings reads the settings through getenv, so a test does not have to touch the environment of the process
func LoadSettings(getenv func(string) string) (Settings, error) {
	port, err := ListenPort(getenv)
	if err != nil {
		return Settings{}, err
	}

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
		Address:     ":" + port,
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

var portDigits = regexp.MustCompile(`^[0-9]{1,5}$`)

// ListenPort reads BUCKET_SIZES_PORT, the port that the metrics listener and the health check of the container use
func ListenPort(getenv func(string) string) (string, error) {
	port := valueOr(getenv("BUCKET_SIZES_PORT"), defaultPort)

	// digits only, because Atoi also takes a sign and the label of the compose service has to be a plain number for Alloy
	number, err := strconv.Atoi(port)
	if !portDigits.MatchString(port) || err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("BUCKET_SIZES_PORT %q is not a port, write a number from 1 to 65535", port)
	}

	return port, nil
}
