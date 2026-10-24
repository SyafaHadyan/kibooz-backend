# Monitoring

This page explains how the metrics and the logs of the API reach Grafana Cloud, what the repository ships for it and how to set it up. Nothing here is needed to run the API, and the stack works the same without it.

## How it fits together

The API serves its metrics on a second listener, `METRICS_ADDRESS`, which `compose.yml` sets to `:9100` and never publishes on the host. Alloy runs next to the API on the private network of the compose file. It finds the containers to watch by the label `kibooz.role` (`api`, `db`, `redis` or `bucket-sizes`), scrapes the metrics listener of each API container and of the bucket size exporter every minute and reads the logs of all of them. It sends both to Grafana Cloud. Because it goes by label and not by name, the containers can have any name, and a second copy of the API needs no change to the Alloy config. The metrics are described in [Operations](operations.md#metrics) and the log lines in [Operations](operations.md#logs).

Alloy has to list containers to find their logs, which needs the Docker socket. Mounting the socket in Alloy would give a compromised Alloy the control of the host, so the socket goes to a proxy instead. The `socket-proxy` container answers read requests for containers and networks and refuses everything else, and it sits on a network with no route out that only Alloy and, when the log archive runs, Vector share. The network list is there because the Docker discovery of Alloy asks for it and stops without it, and it adds the names and settings of the networks, never anything inside a container. A compromised Alloy therefore cannot start, stop or create a container. It can still read what a container shows when it is inspected, which includes the environment of the container with its secrets, such as the key of the log archive and the read key of the bucket size exporter, because Alloy has to inspect a container to read its log through the Docker API. Alloy only talks to the Docker proxy, the API and Grafana Cloud, and it is trusted at the level of the other containers of the stack. The proxy lets a container read everything under `/containers`, which includes the files of a container through the export and archive routes and not only its environment, so the same trust holds for Vector when the log archive runs. Reading the log files of the containers from a read-only mount would avoid the inspection, at the cost of the labels that the dashboards use to tell the copies and hosts apart. Both containers belong to the `monitoring` profile, so they do not start unless that profile is on, and the proxy also starts with the `log-archive` profile.

The Postgres container writes statements that take longer than 500 ms and lock waits longer than one second to its log, and Alloy ships that log as well. The log holds the text of the statement and never the values that were bound to it, because `log_parameter_max_length` is 0.

## More than one copy or host

Every metric and log line carries two labels that say where it came from. `host` is the name of the host, from `GRAFANA_HOST` in `.env`, and `instance` is the name of the container. The dashboards have a `Host` and an `API copy` variable at the top, which show everything by default, and the alerts that watch one copy raise one alert for each copy and name it.

To run a second copy of the API on the same host, give its service the same two labels as `api` in `compose.yml`, `kibooz.role: api` and `kibooz.metrics_port: "9100"` (`"${METRICS_PORT:-9100}"` in `compose.production.example.yml`), whatever its name. The port may also be written like `METRICS_ADDRESS`, as `:9100`. Alloy picks it up within a minute. The copy has to be on the `internal` network of the file, which Alloy shares, and every copy needs its own published port for nginx. A copy may be on other networks as well, such as one that it shares with a database from another stack, and Alloy then scrapes it only through `internal`.

To add another host, run the same compose file with the `monitoring` profile there, with its own `.env` and a `GRAFANA_HOST` that no other host uses. Each host runs its own Alloy and socket proxy, and every host pushes to the same Grafana Cloud stack. Two hosts with the same `GRAFANA_HOST` would mix their series, so pick the name once and keep it.

The series count grows with every copy, because each one has its own set. The free plan limit in the section below counts all of them.

## What the repository holds

| Path | What it is |
|:---|:---|
| `grafana/alloy/config.alloy` | The Alloy pipeline for the metrics and the logs |
| `logarchive/vector.yaml` | The Vector pipeline that archives the logs to a bucket, see [Log archive](#log-archive) |
| `logarchive/.env.example` | The settings of the archive container, which go into `logarchive/.env` |
| `cmd/bucketsizes`, `internal/bucketsizes` | The exporter that measures the size of the buckets, see [Bucket sizes](#bucket-sizes) |
| `bucketsizes/.env.example` | The settings of the exporter, which go into `bucketsizes/.env` |
| `grafana/dashboards/*.json` | API overview, Routes, Dependencies and jobs, Logs, Product activity and Buckets, each with the `Host` variable and, except Buckets, the `API copy` variable. Buckets has a `Bucket` variable instead |
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

The service exists in `compose.production.example.yml` only, so `COMPOSE_PROFILES=log-archive` with `compose.yml` starts nothing. Every 10 minutes, or when 10 MB are ready, it writes one gzip file of JSON lines to `logs/<host>/<year>/<month>/<day>/<time>-<id>.json.gz`, where the folders are the UTC date and not the date in Jakarta, and the time is in seconds since 1970 and the id is random. A line has `timestamp`, `host`, `role` (`api`, `db` or `redis`), `instance` (the container name), `stream` and `message`, which is the line as the container wrote it. Nothing else of the container is archived. The archive is best effort and not a complete record. While the store cannot be reached the lines that Vector already read wait in a disk buffer of 512 MiB in the `kibooz-log-archive` volume, and when that is full Vector stops reading until there is room. Vector keeps no read position of the Docker logs, so the lines that a container writes while Vector is stopped or restarting, or while the buffer is full for longer than Docker keeps its own files (five files of 10 MB), are never archived. Use the logs in Grafana Cloud for the hours around such a gap.

To set it up:

1. Create a private bucket in the store. It must never be public, because the lines hold client addresses and the ids in the paths.
2. Add a lifecycle rule that expires the objects under `logs/` after 90 days. Without it nothing ever deletes them, because the code does not. The rule is a setting of the store, such as a lifecycle rule on a Cloudflare R2 bucket, an expiration rule on an AWS S3 bucket or an ILM or `mc ilm` rule on MinIO.
3. Create a key that is limited to that bucket and give it the least that the store allows. On AWS S3 and MinIO that is a policy with `s3:PutObject` on `logs/*` and nothing else. Cloudflare R2 has no write only token, so use an Object Read and Write token that is scoped to this one bucket. A key that can read and delete can also read and delete the archive, so keep it out of `.env`, which every API container loads. The check at the start of Vector is off, because it asks for the bucket itself and a write only key is refused.
4. Copy `logarchive/.env.example` to `logarchive/.env`, which git ignores, and fill it in. The file is loaded by the archive container only. The address of the store is `LOG_ARCHIVE_ENDPOINT`, such as `https://<account>.r2.cloudflarestorage.com` for Cloudflare R2 or `https://s3.<region>.amazonaws.com` for AWS S3, and the region is `auto` for R2 and the region of the bucket for the others, because AWS refuses `auto`. Then add `log-archive` to `COMPOSE_PROFILES` in `.env`.
5. Start it and check it. After about 10 minutes the bucket holds a first file. The health check of the container only shows that Vector runs and not that the bucket accepts the files, so read the log of the container after the first 10 minutes and after every change of the key.

   ```sh
   docker compose up -d
   docker compose ps log-archive
   docker compose logs --since 15m log-archive
   ```

   A request that the store refuses, such as a wrong key, a bucket that does not exist yet or a wrong region, is logged as an error and tried again, so the lines wait in the disk buffer until the setting is fixed and are not dropped. That buffer is bounded, so fix it within a few hours of a busy host.

Only the owner reads the archive, with a key of their own that can read the bucket and that is never given to the container. Deleting an account does not remove its lines from the archive, which keeps them for at most 90 days like the other copies of the access log.

## Bucket sizes

No call of an S3 compatible store returns the size of a bucket, so a small exporter, `bucketsizes`, lists the objects of each bucket and adds them up. It is a second program in the same image as the API, started by the service `bucket-sizes` of `compose.production.example.yml` under the `bucket-sizes` profile. It lists every bucket at the start and then every hour, keeps the numbers, and serves them on `:9101/metrics` (`BUCKET_SIZES_PORT` in the root `.env`, and not in `bucketsizes/.env`, changes the port, because compose reads the root file to build the label that Alloy uses too) on the private network, so a scrape never lists anything. A listing is one call for each 1000 objects, which some stores charge for, so the interval is an hour by default and cannot be shorter than a minute.

| Metric | Meaning |
|:---|:---|
| `kibooz_bucket_size_bytes{bucket}` | The size of the current objects of the bucket at the last scan that worked. Delete markers, old versions of a versioned bucket and the parts of an upload that was never finished are not counted, so the number can be below what the store bills |
| `kibooz_bucket_objects{bucket}` | The number of objects at the last scan that worked |
| `kibooz_bucket_limit_bytes{bucket}` | The size at which the bucket counts as full, only for the buckets that have a limit in the settings |
| `kibooz_bucket_scan_success{bucket}` | 1 when the last scan worked, 0 when it failed or has not run yet |
| `kibooz_bucket_scan_timestamp_seconds{bucket}` | When the last scan that worked ended |
| `kibooz_bucket_scan_duration_seconds{bucket}` | How long the last scan took, whether it worked or not |

A scan that fails keeps the last size and the time of the last good scan, and sets `kibooz_bucket_scan_success` to 0, and the next bucket is still measured. After every start a bucket shows `kibooz_bucket_scan_success` 0 and no size until its first listing is done, which can take minutes for a bucket with millions of objects. The buckets are listed one after the other, so the last one waits for the sum of the listings. A listing may take 15 minutes by default, which is about 3 million objects, and `BUCKET_SIZES_SCAN_TIMEOUT` raises that. The interval is the pause after a scan, so a slow listing is never followed by another one at once. Listing is one Class A call for each 1000 objects on Cloudflare R2, so an hourly scan of a bucket with a million objects is about 720 thousand calls a month, which counts against the free tier of a million.

To set it up:

1. Create a key for the exporter that may list the buckets, and nothing else. The stores differ, so give it the least they allow, for Cloudflare R2 an Object Read only token that is scoped to the buckets to measure. Keep it out of `.env`, which every API container loads.
2. Copy `bucketsizes/.env.example` to `bucketsizes/.env`, which git ignores, and fill it in. `BUCKET_SIZES_BUCKET_NAMES` lists the buckets, each with an optional size at which it counts as full, such as `media,chat=5GB,logs=20GB`. The exporter works with any S3 compatible store, because the address of the store is `BUCKET_SIZES_ENDPOINT`.
3. Add `bucket-sizes` to `COMPOSE_PROFILES` in `.env`, next to `monitoring`, because without that profile nothing scrapes the exporter. Start it and check it.

   ```sh
   docker compose up -d bucket-sizes
   docker compose logs --since 5m bucket-sizes
   docker compose exec bucket-sizes /bucketsizes healthcheck
   ```

Alloy scrapes the exporter like the API, by the label `kibooz.role: bucket-sizes`, so no setting of Alloy changes. Its metrics carry the label `service="kibooz-bucket-sizes"`. The dashboard Buckets shows the sizes and the alerts below watch them.

The exporter runs the image of `IMAGE_TAG`, and `scripts/rolling-update.sh` only restarts the API copies unless you name the exporter, so run it with `EXTRAS=bucket-sizes` (or `EXTRAS=all`) or run `docker compose up -d bucket-sizes` after a deploy to bring it to the same version.

## Errors in Sentry

Grafana says that the API is unhealthy, and Sentry says what broke. The API sends a failure of the server, a 5xx or a panic, to Sentry when `SENTRY_DSN` is set. The report holds the error text, the stack, the route pattern, the request id and the id of the account, and the request id is how a report is matched to its line in the logs. A 4xx is part of the API and a 503 means that something such as the storage is not set up, so neither is sent. See [ADR 0006](adr/0006-sentry-error-reports.md) for what is sent and what never is.

To set it up, create a project for the Go platform with error monitoring only, copy its DSN from the client keys of the project into `SENTRY_DSN` of the `.env` of the server, and restart the API. Then turn on the data scrubber with its default scrubbers and the setting that prevents storing IP addresses in the security and privacy settings of the organization, and turn on spike protection. The limit for each client key is a feature of the Business and Enterprise plans, so the cap of the code, `SENTRY_EVENTS_PER_MINUTE`, limits how fast a crash loop can send. It is a burst limit and not a monthly budget, because two copies at the default of 30 allow 3600 events an hour and the free plan has 5000 errors a month, so spike protection stays on as well. A host that runs more than one stack keeps one project and gives each stack its own `SENTRY_ENVIRONMENT`.

## Keep it inside the free plan

The Grafana Cloud free plan allows about 10 000 active series and 50 GB of logs a month. The API has a few thousand series for each copy, mostly the buckets of the latency histogram for each route, so a new route adds about 15 series for each method and copy. The logs are the larger cost, because every request is one line. A health check that answers `200` is written below the default level and is not logged. If the logs come close to the limit, stop collecting the Redis container first, by giving it no `kibooz.role` label, or raise the level of the access log.

## What the alerts watch

| Alert | Fires when |
|:---|:---|
| A bucket is nearly full | A bucket holds more than 80 percent of the limit that was set for it in `BUCKET_SIZES_BUCKET_NAMES`, for 10 minutes. A bucket without a limit never fires it |
| A bucket scan is failing | No scan of a bucket worked in the last 2 hours, which is two scans in a row at the default interval, so one failed call does not fire it. A start of the exporter does not fire it either, because the window still holds the values from before the start |
| A bucket scan is old | The size of a bucket was last measured more than 3 hours ago. The rule looks back a day, so an exporter that was stopped or removed fires it after 3 hours and keeps it for a day. A bucket that never worked has no timestamp, and the rule above covers it |
| The bucket size exporter does not answer | The container of the exporter runs, but its metrics were not scraped for 10 minutes. A container that was stopped or removed disappears from the discovery of Alloy and has no series at all, so that case is caught by the rule above and not by this one. A host without the `bucket-sizes` profile raises none of these rules |
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

The thresholds of the bucket rules (a look back of 2 hours, an age of 3 hours) assume the default interval of one hour, so raise them in the rules when `BUCKET_SIZES_INTERVAL` is longer than that. The share of the limit uses a look back of 3 hours, so it keeps working while the exporter restarts and lists its buckets again.

The rules marked "of a copy" raise one alert for each copy, and the message names the copy and its host. The others look at the service as a whole. A job that runs but has not succeeded yet counts from the moment the API started, so one that never succeeds fires after the time in the table. A job that is switched off, such as the file cleanup when the bucket is not configured, has no series and no alert.

The Better Stack monitors in [Operations](operations.md#uptime-monitoring) stay as they are. They look at the public address from outside, which these alerts cannot do from inside the host.
