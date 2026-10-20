# 0009 Monitoring goes to Grafana Cloud through Alloy, found by Docker labels

Status: Accepted, 2026-10-21. This record was written after the fact. [monitoring.md](../monitoring.md) says how to set it up.

## Context

The API runs on one small VPS, and later it may run on more than one host or as more than one copy. Someone has to see its health, its latency by route and its logs without logging into the server, and be told when something is wrong. There is no budget for a monitoring server of our own, and the host has the Docker socket, which gives full control of the machine to whoever can use it.

## Decision

The API serves Prometheus metrics on a second listener, `METRICS_ADDRESS`, which `compose.yml` sets to `:9100` on the private network and never publishes on the host. The access log and every other line are JSON, with a request id, a level, the route and the reason of each failure.

Grafana Alloy runs next to the API and sends the metrics and the logs to Grafana Cloud, on the free plan. It finds the containers by the Docker labels `kibooz.role` (api, db or redis) and `kibooz.metrics_port` and not by name, so a container can have any name. A second copy on the same host needs the same two labels, the `internal` network of the compose file and a port of its own. A copy on another host also needs the monitoring profile and its own `GRAFANA_HOST`. Every series and log line carries `host` (from `GRAFANA_HOST`) and `instance` (the name of the container), so copies never collide.

Alloy lists containers through a socket proxy that answers read requests for containers and networks and refuses everything else, so a compromised Alloy cannot control the host. It can still inspect a container, and the inspection shows the environment of that container with its secrets. That risk is accepted, because Alloy is trusted at the level of the other containers of the stack, and reading the log files from a read-only mount would avoid it at the cost of the labels that tell the copies apart, see [monitoring.md](../monitoring.md). It scrapes only the network of the compose file, and `honor_labels` keeps the `job` label of the background jobs.

The dashboards and the alert rules are JSON files in `grafana/`. They are not synced from the repository. They are copied to Grafana by hand, and the live copy has the data source ids that the repository file does not. The alerts go to one email contact point. Uptime from outside is a separate service (Better Stack), and errors with stacks go to Sentry ([0006](0006-sentry-error-reports.md)).

## Consequences

- The free plan allows about 10 000 active series and 50 GB of logs a month, so the labels of a metric are chosen with care and a route label that grows with the input is never added.
- A change in the dashboards or the alerts has to be made in both places, and the other one drifts until someone copies it.
- The Alloy file on a server that was edited by hand has to be checked out again before a pull, and Alloy restarted once, because it reads the file only at start.
- The metrics listener must stay off the public network. Anyone who can reach it can read every route and count.
- Alloy depends on its socket proxy, so an outage of the proxy stops the logs and the discovery, while the metrics of the API itself are only cached for a short time.
