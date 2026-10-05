package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// runHealthcheck probes the local server so container healthchecks work in an image without curl
func runHealthcheck() int {
	port := os.Getenv("APP_PORT")
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		port = "8080"
	}

	client := http.Client{Timeout: 3 * time.Second}

	//nolint:gosec // the host is fixed to loopback and the port is validated as a number
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/healthz", port))
	if err != nil {
		return 1
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return 1
	}

	return 0
}
