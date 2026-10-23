# Monitoring

This page explains how the metrics and the logs of the API reach Grafana Cloud, what the repository ships for it and how to set it up. Nothing here is needed to run the API, and the stack works the same without it.

## How it fits together

The API serves its metrics on a second listener, `METRICS_ADDRESS`, which `compose.yml` sets to `:9100` and never publishes on the host. Alloy runs next to the API on the private network of the compose file. It finds the containers to watch by the label `kibooz.role` (`api`, `db` or `redis`), scrapes the metrics listener of each API container every minute and reads the logs of all of them. It sends both to Grafana Cloud. Because it goes by label and not by name, the containers can have any name, and a second copy of the API needs no change to the Alloy config. The metrics are described in [Operations](operations.md#metrics) and the log lines in [Operations](operations.md#logs).

Alloy has to list containers to find their logs, which needs the Docker socket. Mounting the socket in Alloy would give a compromised Alloy the control of the host, so the socket goes to a proxy instead. The `socket-proxy` container answers read requests for containers and networks and refuses everything else, and it sits on a network with no route out that only Alloy shares. The network list is there because the Docker discovery of Alloy asks for it and stops without it, and it adds the names and settings of the networks, never anything inside a container. A compromised Alloy therefore cannot start, stop or create a container. It can still read what a container shows when it is inspected, which includes the environment of the container with its secrets, because Alloy has to inspect a container to read its log through the Docker API. Alloy only talks to the Docker proxy, the API and Grafana Cloud, and it is trusted at the level of the other containers of the stack. Reading the log files of the containers from a read-only mount would avoid the inspection, at the cost of the labels that the dashboards use to tell the copies and hosts apart. Both containers belong to the `monitoring` profile, so they do not start unless that profile is on.

The Postgres container writes statements that take longer than 500 ms and lock waits longer than one second to its log, and Alloy ships that log as well. The log holds the text of the statement and never the values that were bound to it, because `log_parameter_max_length` is 0.

## More than one copy or host

Every metric and log line carries two labels that say where it came from. `host` is the name of the host, from `GRAFANA_HOST` in `.env`, and `instance` is the name of the container. The dashboards have a `Host` and an `API copy` variable at the top, which show everything by default, and the alerts that watch one copy raise one alert for each copy and name it.

To run a second copy of the API on the same host, give its service the same two labels as `api` in `compose.yml`, `kibooz.role: api` and `kibooz.metrics_port: "9100"`, whatever its name. The port may also be written like `METRICS_ADDRESS`, as `:9100`. Alloy picks it up within a minute. The copy has to be on the `internal` network of the file, which Alloy shares, and every copy needs its own published port for nginx. A copy may be on other networks as well, such as one that it shares with a database from another stack, and Alloy then scrapes it only through `internal`.

To add another host, run the same compose file with the `monitoring` profile there, with its own `.env` and a `GRAFANA_HOST` that no other host uses. Each host runs its own Alloy and socket proxy, and every host pushes to the same Grafana Cloud stack. Two hosts with the same `GRAFANA_HOST` would mix their series, so pick the name once and keep it.

The series count grows with every copy, because each one has its own set. The free plan limit in the section below counts all of them.

## What the repository holds

| Path | What it is |
|:---|:---|
| `grafana/alloy/config.alloy` | The Alloy pipeline for the metrics and the logs |
| `grafana/dashboards/*.json` | API overview, Routes, Dependencies and jobs, Logs and Product activity, each with the `Host` and `API copy` variables |
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

5. After a few minutes the Explore view of the Prometheus data source answers `up{service="kibooz-api"}` with `1`, and the Loki data source answers `{service_name="kibooz", role="api"}` with lines. Both carry the `host` label from `GRAFANA_HOST` and an `instance` label with the container name.
6. Set the scrape interval of the Prometheus data source to `1m` in its settings, which is how often Alloy scrapes. The dashboards also set a minimum step of one minute on the queries that take a rate over the interval of the graph, so those rates span at least two samples. The two tables of the Routes dashboard take their rate over the whole time range of the dashboard, so choose a range of a few minutes or more for them. Then import the dashboards from `grafana/dashboards` and choose the two data sources when asked.
7. Create a contact point that reaches the owner and a notification policy that uses it, then create the alert rules. Replace `__PROMETHEUS_UID__` in `grafana/alerts/rules.json` with the id of the Prometheus data source and send each rule to the provisioning API. A folder with the id `kibooz` has to exist first.

Alloy keeps a small write-ahead log in the `kibooz-alloy` volume, so a short outage of the network does not lose samples.

## Log archive

Grafana Cloud keeps the logs for 14 days on the free plan and the host keeps a few hours, so the logs go to a bucket as well, where they stay for 90 days. A second container, `log-archive`, runs Vector (`logarchive/vector.yaml`). It reads the logs of the same containers as Alloy through the same socket proxy, and writes them to any S3 compatible store, so the store is a setting and not a choice of the code. It starts only with the `log-archive` profile, next to `monitoring` or alone.

Every 10 minutes, or when 10 MB are ready, it writes one gzip file of JSON lines to `logs/<host>/<year>/<month>/<day>/<time>-<id>.json.gz`. A line has `timestamp`, `host`, `role` (`api`, `db` or `redis`), `instance` (the container name), `stream` and `message`, which is the line as the container wrote it. Nothing else of the container is archived. The archive is best effort and not a complete record. While the store cannot be reached the lines that Vector already read wait in a disk buffer of 512 MiB in the `kibooz-log-archive` volume, and when that is full Vector stops reading until there is room. Vector keeps no read position of the Docker logs, so the lines that a container writes while Vector is stopped or restarting, or while the buffer is full for longer than Docker keeps its own files (five files of 10 MB), are never archived. Use the logs in Grafana Cloud for the hours around such a gap.

To set it up:

1. Create a private bucket in the store. It must never be public, because the lines hold client addresses and the ids in the paths.
2. Add a lifecycle rule that expires the objects under `logs/` after 90 days. Without it nothing ever deletes them, because the code does not. The rule is a setting of the store, such as a lifecycle rule on a Cloudflare R2 bucket, an expiration rule on an AWS S3 bucket or an ILM or `mc ilm` rule on MinIO.
3. Create a key that may only write objects to that bucket, with no read and no list. The check at the start of Vector is off, because it asks for the bucket itself and a write only key is refused, so a wrong bucket or key shows at the first write, in `docker compose logs log-archive`.
4. Add the `LOG_ARCHIVE_*` values to `.env`, as listed in `.env.example`, and add `log-archive` to `COMPOSE_PROFILES`. The address of the store is `LOG_ARCHIVE_ENDPOINT`, such as `https://<account>.r2.cloudflarestorage.com` for Cloudflare R2 or `https://s3.<region>.amazonaws.com` for AWS S3, and the region is `auto` for R2 and the region of the bucket for the others.
5. Start it and check it. After about 10 minutes the bucket holds a first file.

   ```sh
   docker compose up -d
   docker compose ps log-archive
   docker compose logs --since 15m log-archive
   ```

Only the owner reads the archive, with a key of their own that can read the bucket and that is never given to the container. Deleting an account does not remove its lines from the archive, which keeps them for at most 90 days like the other copies of the access log.

## Errors in Sentry

Grafana says that the API is unhealthy, and Sentry says what broke. The API sends a failure of the server, a 5xx or a panic, to Sentry when `SENTRY_DSN` is set. The report holds the error text, the stack, the route pattern, the request id and the id of the account, and the request id is how a report is matched to its line in the logs. A 4xx is part of the API and a 503 means that something such as the storage is not set up, so neither is sent. See [ADR 0006](adr/0006-sentry-error-reports.md) for what is sent and what never is.

To set it up, create a project for the Go platform with error monitoring only, copy its DSN from the client keys of the project into `SENTRY_DSN` of the `.env` of the server, and restart the API. Then turn on the data scrubber with its default scrubbers and the setting that prevents storing IP addresses in the security and privacy settings of the organization, and turn on spike protection. The limit for each client key is a feature of the Business and Enterprise plans, so the cap of the code, `SENTRY_EVENTS_PER_MINUTE`, limits how fast a crash loop can send. It is a burst limit and not a monthly budget, because two copies at the default of 30 allow 3600 events an hour and the free plan has 5000 errors a month, so spike protection stays on as well. A host that runs more than one stack keeps one project and gives each stack its own `SENTRY_ENVIRONMENT`.

## Keep it inside the free plan

The Grafana Cloud free plan allows about 10 000 active series and 50 GB of logs a month. The API has a few thousand series for each copy, mostly the buckets of the latency histogram for each route, so a new route adds about 15 series for each method and copy. The logs are the larger cost, because every request is one line. A health check that answers `200` is written below the default level and is not logged. If the logs come close to the limit, stop collecting the Redis container first, by giving it no `kibooz.role` label, or raise the level of the access log.

## What the alerts watch

| Alert | Fires when |
|:---|:---|
| API is down | The metrics of a copy were not scraped for 5 minutes, which means the API or its metrics listener is not answering. It also fires when no data arrives at all, which is what happens when every Alloy is down |
| Fewer than 2 API copies are up | Fewer than 2 copies of the API reported `up` for 5 minutes, because a container stopped or was removed or the Alloy of a host stopped. It counts the copies that are up, so it does not name the missing one, and the "API copies up" panel of the API overview dashboard shows how many are up. A rename does not fire it. Removing a copy on purpose stays quiet only while at least two copies are still up, so removing one of the two for good fires it after 5 minutes until the threshold in the rule is lowered. The count is 0 when no copy reports at all, so it fires then as well |
| 5xx ratio is high | More than 5 percent of the requests failed with a 5xx for 10 minutes |
| p95 latency is high | The p95 latency of the requests other than the health check, which 95 percent of them stay under, was above one second for 10 minutes. The health check is left out because its storage write probe takes half a second or more on purpose, and with no other traffic it alone would set the percentile. A rule with no requests to measure stays quiet |
| A background job has not succeeded | The semester check or a keepalive of a copy has not succeeded for 10 minutes |
| A slow background job has not succeeded | The file cleanup, the idempotency purge or the refresh token purge of a copy has not succeeded for 3 hours |
| Background jobs are failing | A job of a copy failed more than 3 times in 30 minutes |
| Password hashing is saturated | A copy refused more than 5 requests in 10 minutes because every hashing slot was taken |
| Database pool is nearly full | More than 90 percent of the 25 connections of a copy were in use for 5 minutes |
| Many requests are rate limited | More than 100 requests were refused by a limiter in 10 minutes |
| An API copy keeps restarting | A copy started more than once in 30 minutes, which is what a crash loop or an out of memory kill looks like. The "API is down" alert alone can miss it, because a copy that comes back every few minutes is scraped again before 5 minutes pass. It sees a restart only when the copy ran long enough to be scraped, so a loop that dies within a minute is covered by "Fewer than 2 API copies are up" alone. A deploy followed by a rollback within 30 minutes also fires it, on purpose |
| API memory is close to its limit | The resident memory of a copy was above 300 MiB for 5 minutes. The default limit of a copy is 384 MiB, so this warns before the container is killed. Set a different threshold in the rule when `API_MEMORY_LIMIT` is not the default |
| Push wake-ups are failing or dropped | A copy could not send or had to drop more than 5 chat wake-up pushes in 10 minutes. A copy that tries fewer than 6 in that time stays quiet even when every one fails |

The rules marked "of a copy" raise one alert for each copy, and the message names the copy and its host. The others look at the service as a whole. A job that runs but has not succeeded yet counts from the moment the API started, so one that never succeeds fires after the time in the table. A job that is switched off, such as the file cleanup when the bucket is not configured, has no series and no alert.

The Better Stack monitors in [Operations](operations.md#uptime-monitoring) stay as they are. They look at the public address from outside, which these alerts cannot do from inside the host.
