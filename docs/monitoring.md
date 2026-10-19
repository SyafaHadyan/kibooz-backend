# Monitoring

This page explains how the metrics and the logs of the API reach Grafana Cloud, what the repository ships for it and how to set it up. Nothing here is needed to run the API, and the stack works the same without it.

## How it fits together

The API serves its metrics on a second listener, `METRICS_ADDRESS`, which `compose.yml` sets to `:9100` and never publishes on the host. Alloy runs next to the API on the private network of the compose file, scrapes `api:9100/metrics` every minute and reads the logs of the `kibooz-api`, `kibooz-db` and `kibooz-redis` containers. It sends both to Grafana Cloud. The metrics are described in [Operations](operations.md#metrics) and the log lines in [Operations](operations.md#logs).

Alloy has to list containers to find their logs, which needs the Docker socket. Mounting the socket in Alloy would give a compromised Alloy the control of the host, so the socket goes to a proxy instead. The `socket-proxy` container answers read requests for containers and refuses everything else, and it sits on a network with no route out that only Alloy shares. Both containers belong to the `monitoring` profile, so they do not start unless that profile is on.

The Postgres container writes statements that take longer than 500 ms and lock waits longer than one second to its log, and Alloy ships that log as well.

## What the repository holds

| Path | What it is |
|:---|:---|
| `grafana/alloy/config.alloy` | The Alloy pipeline for the metrics and the logs |
| `grafana/dashboards/*.json` | API overview, Routes, Dependencies and jobs, Logs and Product activity |
| `grafana/alerts/rules.json` | The alert rules, as an array in the format of the alert rule provisioning API |

The dashboards pick their data sources with two variables at the top (`Metrics` and `Logs`), so they hold no data source id. The alert rules hold the placeholder `__PROMETHEUS_UID__` in place of the id of the metrics data source, and it has to be replaced before they are applied. Grafana is not synced from this repository. The JSON files are the record of what was built, and a change made in Grafana is copied back here by hand.

## Set it up

1. In the Grafana Cloud stack, open the connection details of the Prometheus and the Loki data source and note the push URL and the user (instance id) of each.
2. Create an access policy for the stack with the scopes `metrics:write` and `logs:write`, then create a token for it. The token is shown once.
3. Add the values to `.env` on the host, next to `COMPOSE_PROFILES=monitoring`. The names are listed in `.env.example`.
4. Start the two new containers and check that Alloy runs.

   ```sh
   docker compose up -d
   docker compose logs --since 5m alloy
   ```

5. After a few minutes the Explore view of the Prometheus data source answers `up{service="kibooz-api"}` with `1`, and the Loki data source answers `{container="kibooz-api"}` with lines.
6. Import the dashboards from `grafana/dashboards`, and choose the two data sources when asked.
7. Create a contact point that reaches the owner and a notification policy that uses it, then create the alert rules. Replace `__PROMETHEUS_UID__` in `grafana/alerts/rules.json` with the id of the Prometheus data source and send each rule to the provisioning API. A folder with the id `kibooz` has to exist first.

Alloy keeps a small write-ahead log in the `kibooz-alloy` volume, so a short outage of the network does not lose samples.

## Keep it inside the free plan

The Grafana Cloud free plan allows about 10 000 active series and 50 GB of logs a month. The API has a few thousand series, mostly the buckets of the latency histogram for each route, so a new route adds about 15 series for each method. The logs are the larger cost, because every request is one line. A health check that answers `200` is written below the default level and is not logged. If the logs come close to the limit, drop the `kibooz-redis` container in `discovery.relabel` first, or raise the level of the access log.

## What the alerts watch

| Alert | Fires when |
|:---|:---|
| API is down | The metrics were not scraped for 5 minutes. It also fires when Alloy itself is down, which is why missing data counts as firing |
| 5xx ratio is high | More than 5 percent of the requests failed with a 5xx for 10 minutes |
| p95 latency is high | The p95 latency, which 95 percent of the requests stay under, was above one second for 10 minutes |
| A background job has not succeeded | The semester check or a keepalive has not succeeded for 10 minutes |
| A slow background job has not succeeded | The file cleanup or the idempotency purge has not succeeded for 3 hours |
| Background jobs are failing | A job failed more than 3 times in 30 minutes |
| Password hashing is saturated | More than 5 requests were refused in 10 minutes because every hashing slot was taken |
| Database pool is nearly full | More than 90 percent of the 25 connections were in use for 5 minutes |
| Many requests are rate limited | More than 100 requests were refused by a limiter in 10 minutes |

The job alerts stay quiet for a job that has never succeeded, such as the file cleanup when the bucket is not configured, because there is no sample to age. The failure alert covers a job that runs and fails.

The Better Stack monitors in [Operations](operations.md#uptime-monitoring) stay as they are. They look at the public address from outside, which these alerts cannot do from inside the host.
