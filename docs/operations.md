# Operations

This page is for whoever runs the stack. It covers deploying a version, what the server does on its own, where to look when something is wrong and how to go back. The settings are listed in the [README](../README.md#configuration), and the reasons behind them are in the [design notes](design.md).

## What runs

The `compose.yml` file starts three containers on one host. `kibooz-api` is the image `syafa/kibooz-backend` and listens on `127.0.0.1:${APP_PORT}`, so only a reverse proxy on the same host reaches it. `kibooz-db` is PostgreSQL 18 and `kibooz-redis` is Redis 8. The API container has a read-only filesystem, drops every Linux capability and runs without new privileges. Each container keeps at most five log files of 10 MB. The API and Redis containers have a hard memory cap (`API_MEMORY_LIMIT`, `REDIS_MEMORY_LIMIT`) and the database has none, so a memory problem of the API kills the API and not the database. The API gets 40 seconds to stop, because it gives running requests 15 seconds and then up to 17 more to close the push, metrics and error report work, and the database gets 30 seconds because PostgreSQL ends with a checkpoint.

Two more containers, `kibooz-alloy` and `kibooz-socket-proxy`, start only when `COMPOSE_PROFILES` holds `monitoring`. They ship the metrics and the logs to Grafana Cloud, see [Monitoring](monitoring.md).

A hosted PostgreSQL, Redis or bucket can replace the bundled ones by pointing `DB_*`, `REDIS_*` and `S3_*` at them. Redis and the bucket are optional, see [Redis is optional](design.md#redis-is-optional).

## Deploy a version

1. Read the changelog of the release for new settings or a migration that needs care.
2. Set `IMAGE_TAG` in `.env` to the version without a `v`, for example `0.11.0`. The default `latest` follows `main`, and a pinned version makes a rollback a one line change.
3. Pull and restart.

   ```sh
   docker compose pull api
   docker compose up -d
   ```

4. Check that the new version answers.

   ```sh
   curl -s https://<api host>/healthz
   ```

   The body has `status` `ok` and the `version` of the release. `degraded` means Redis is down or the bucket refuses writes, and the API still serves. `down` comes with a `503` and means the database does not answer.
5. Run the manual `Smoke` workflow from the Actions tab with `expected_version` set to the new version. It registers throwaway accounts, replays the main routes and removes the accounts again, see [CI/CD](ci-cd.md).

A host that runs two copies of the API behind a reverse proxy can update them one at a time with `scripts/rolling-update.sh`, so one copy always answers. `compose.production.example.yml` is a file with two copies, each capped at half a CPU, that the script can work with, and CI keeps it valid. The host needs Docker with the compose plugin, curl, jq and flock, a compose file with one service for each copy (`api-1` on port 8000 and `api-2` on port 8001 by default), and a proxy that stops sending requests to a copy that is down. Copy the script next to the compose file and run it in tmux or screen with `./rolling-update.sh 0.17.1`. It checks that the services exist, pulls the image of the version, which also proves that the tag exists, and only then sets `IMAGE_TAG`. For each copy it pulls through compose, recreates that copy with no dependencies and polls its `/healthz` until the status is `ok`, every check is `ok` or `disabled` and the version is the one asked for, twice in a row, for at most two minutes by default. Only then does it update the next copy.

When the pull through compose fails, for example because Docker Hub limited it, or when you interrupt the script before the first copy is recreated, the script puts the env file back as it was and stops, so the file never names a version that no copy runs after the script has stopped, which a later `docker compose up -d` would start on both copies at once. While the script runs the file does name the new version before the first copy is recreated, so do not run `docker compose up -d` yourself until it ends. When `docker compose up` fails for a copy, or a copy does not become healthy, `IMAGE_TAG` stays on the new version, because that copy may already run it, and the script says so and warns against `docker compose up -d` until the copies are checked. When a copy does not become healthy the script stops and says which copies are already new, which are still old and how to go back. It never rolls back by itself, because the new version may already have applied a migration, and an old image cannot start on a newer schema. A rolling update is therefore only safe when the migrations of the new version leave the old version working, since both run on the same database for a short while, so read the changelog first. The script also goes back. Give it an older version and it warns that this is only safe while no migration ran since that version, then lists the migration files of both versions on GitHub. When the newer version has files that the older one lacks, or GitHub cannot answer (a rate limit, a missing tag, no network), it stops, because the older image cannot start on a newer schema. Restore the database or apply the down files as described under Roll back, or check by hand that the `schema_migrations` table is not past the newest migration of the older version, and run it again with `MIGRATIONS_HANDLED=1`. Setting `GITHUB_TOKEN` lifts the anonymous limit of 60 requests an hour. When nothing blocks it, the script asks you to type the version to go on, or to set `ROLLBACK_OK` to the version when there is no terminal. The version that runs now is the newest one that any copy reports, so a half finished update is judged by its newest copy. When it cannot tell which version runs now, because `IMAGE_TAG` is `latest` and the first copy does not answer, it says so and cannot warn. In a real run a tag that does not exist stops the script before anything is changed, and a dry run only prints the pull. The `COPIES`, `COMPOSE_DIR`, `ENV_FILE`, `IMAGE`, `REPO`, `TIMEOUT` and `DRY_RUN` variables change the defaults, see the comment at the top of the script.

A restart stops accepting new requests and gives running ones 15 seconds to finish. Clients that lose a request in that window repeat it, and the routes that create something accept an `Idempotency-Key` so a repeat is safe.

## Migrations

The schema is built from SQL files embedded in the binary. The server applies every pending file with golang-migrate when it starts, before it listens, so there is no step to run by hand and a deploy that adds a migration needs nothing extra. The migrator takes a database lock, so two instances that start together do not apply the same file twice.

- Files are append only. A schema change is a new numbered file and never an edit of an old one, because other databases have already applied the old one. A test fails when a number is missing or used twice.
- Every migration has an up and a down file.
- A stop signal that arrives while the API starts, which includes the migrations, waits until the start has finished and the process then exits without listening, so the migration that is running is not cut off. A second signal ends the process at once. If the stop grace period of the container ends first, the process is killed anyway and a migration that was running leaves the database dirty. A copy that only waits for the migration lock of another copy also holds the signal, so its `docker stop` can take the whole grace period.
- A migration that fails halfway leaves the database marked dirty, and the next start stops with `Dirty database version`. Fix the cause, repair the data by hand if needed, and clear the mark with `migrate force <version>` from the golang-migrate command line before starting again. A migration file runs as one statement batch, so a failure rolls the whole file back and the version to force is the one before it, for example `migrate force 23` after a failure of migration 24, and forcing the failed version itself would skip the file.

## Settings that need care

| Setting | Why it matters |
|:---|:---|
| `JWT_SECRET_KEY` | Changing it makes every access token and device token stop verifying. Refresh tokens are random values stored in the database, so apps that refresh keep their sessions, but a device that sends an old device token falls back to the shared login bucket of its email. Keep it at least 32 characters and out of the repository |
| `APP_TIMEZONE` | Decides what "today" is, the Monday to Friday chart and the day the semester starts |
| `SEMESTER_START_MONTHS` | On the first day of each listed month the leaderboard of every class resets and the final standings are saved |
| `TRUST_PROXY`, `PROXY_HEADER` | Only the access log reads the client address, no limit does. Set them when a proxy sits in front, or every request is logged with the proxy address |
| `IDEMPOTENCY_TTL_HOURS` | How long a retried request gets the first answer again. Lowering it shortens the window in which an offline app can retry safely |
| `SENTRY_DSN`, `SENTRY_EVENTS_PER_MINUTE` | Error reports leave only when the DSN is set. The cap is the most reports in a minute, and the log says `error reports are being dropped by the cap` when a storm passes it. A DSN that cannot be read is logged and leaves the reports off, it never stops the API. A compose file that lists its settings one by one needs these lines too |
| `S3_*` | Uploads stay off until all of them are set. The bucket needs a lifecycle rule that expires `pending/` after one day and never `videos/` |
| `S3_PRIVATE_BUCKET_NAME` | The bucket of the chat photos and videos. It must not be the public bucket `S3_PUBLIC_BUCKET_NAME`, which the server refuses at start, and the key of `S3_*` has to cover it. It needs a lifecycle rule that expires `pending/` after one day and never `chat/`, and chat media stays off while the name is empty. `/healthz` writes a probe object to it as well, so a private bucket that refuses writes shows as `degraded` |
| `FIREBASE_CREDENTIALS_FILE` | The path of the service account key inside the container. Mount the file read only, for example `./secrets/firebase.json:/run/secrets/firebase.json:ro` in a compose override, and set the path to `/run/secrets/firebase.json`. Create the file on the host first, because Docker makes a folder of a mount whose source is missing. The key never goes into the repository, the image or a chat. A path that cannot be read, or a file that is not a service account key, is logged as `push notifications are off because the Firebase key cannot be used` and the API starts without pushes. `push notifications are on` says it works |
| `CHAT_MAX_SOCKETS`, `CHAT_MAX_SOCKETS_PER_USER` | How many chat sockets one API copy holds, 1000 by default, and how many one account keeps open, 5 by default. A socket takes a few tens of KiB, so keep the first below what the memory cap leaves after the normal load, and watch `kibooz_chat_sockets`. The reverse proxy has to pass the WebSocket upgrade and keep an idle connection for more than 60 seconds, see [Chat socket](websocket.md#reverse-proxy) |
| `UPLOAD_CONCURRENCY` | How many photo uploads (the avatar, a scan claim with a photo and a chat photo) one API copy handles at the same time, 4 by default. A photo is held in memory as the body, a decoded copy and a cleaned copy, so keep it low for a small memory cap. A request with a body of 64 KB or less holds no photo and never takes a slot. The next upload gets `429` with `Retry-After: 1`, and the gate bounds the work after the body arrives and not the bodies, which the proxy has to limit |
| `UPLOAD_CONCURRENCY_PER_ACCOUNT` | How many of those uploads one account may have running in one API copy, 2 by default and at least 1. It stops a client that loops, or one account on several devices, from taking every slot of the copy from the others. It does not stop many accounts, so the limit on the connections in the proxy still matters. The refused upload gets the same `429` with `Retry-After: 1` and a message that a photo of the account is still being received |

## Background work

The server runs five jobs by itself, and none of them needs a scheduler outside the process.

| Job | Every | What it does |
|:---|:---|:---|
| Semester check | 1 minute | Starts a new semester when the calendar says so, then starts it for every class that has not started it yet, one class at a time, saving its final standings and clearing its leaderboard cache. An instance that stops halfway is continued by the next run of any instance |
| File cleanup | 10 minutes | Removes bucket files that a deleted account left behind and retries the ones that failed. Only runs when storage is configured |
| Idempotency purge | 1 hour | Deletes remembered keys older than `IDEMPOTENCY_TTL_HOURS` |
| Refresh token purge | 1 hour, and the first run 1 minute after the start | Deletes, for every account, the refresh tokens that no longer matter, which are the ones that expired unused and the used ones whose session reached `JWT_SESSION_MAX_DAYS`. A token that another request holds is skipped and taken by the next run. The first run does one pass over the whole table, so on a large table count the rows with `SELECT count(*) FROM refresh_tokens` first |
| Keepalive | `KEEPALIVE_SECONDS` | Sends `SELECT 1` to the database and `PING` to Redis, so hosted instances that pause when idle stay awake |

## Metrics

The API counts what it does and shows it in the Prometheus text format. The page is served by a second listener on `METRICS_ADDRESS` (for example `:9100`), which answers `GET /metrics` and nothing else. The setting is empty by default, which switches the listener off. The public port never serves `/metrics`, and the listener has no authentication, so its port must not be published on the host and the reverse proxy must not point at it. Only a scraper on the private network of the deployment should reach it. An address that is malformed, that is taken or that equals `APP_PORT` stops the start-up.

| Metric | Labels | What it shows |
|:---|:---|:---|
| `kibooz_http_requests_total` | `method`, `route`, `status` | Every request that reaches the middleware, which is every request except two kinds that Fiber answers before any middleware runs, a body over `BODY_LIMIT_MB` (`413`) and a method that Fiber does not know (`501`). `route` is the path a route was registered with, such as `/api/v1/wali/children/:studentId`. A request that the sign-in or role check of a group refuses, such as a `401` on `/api/v1/wali`, shows the path of the group. A request that matched no route, or that a check of the whole app such as the 64 KB JSON body limit refused, counts under `unmatched`, so a client cannot create series with the paths it sends |
| `kibooz_http_request_duration_seconds` | `method`, `route` | A histogram of the time to answer |
| `kibooz_http_requests_in_flight` | none | Requests that are running now |
| `kibooz_rate_limited_total` | `route` | Requests that a rate limiter refused with `429 RATE_LIMITED`, and the photo uploads that the upload gate refused because the copy already handled `UPLOAD_CONCURRENCY` of them or the account already had `UPLOAD_CONCURRENCY_PER_ACCOUNT` of them running |
| `kibooz_chat_sockets` | none | Chat sockets that are open on this copy, which stays below `CHAT_MAX_SOCKETS` |
| `kibooz_chat_events_delivered_total` | `type` | Events written to the chat sockets of this copy, by type |
| `kibooz_chat_event_gaps_total` | none | Gaps that this copy found in the numbered events that another copy published, each one makes the sockets of this copy resync |
| `kibooz_chat_bus_publish_failed_total` | none | Chat events and heartbeats that this copy could not publish to Redis, the sockets of the other copies then hear about an event only from the app that asks again |
| `kibooz_chat_bus_publish_dropped_total` | none | Chat events and heartbeats that this copy dropped from its queue for Redis, either because the queue of 256 was full, which needs a slow Redis while many events arrive, or because the process stopped and the two seconds of the last drain ran out. The other copies find the skipped numbers and resync, so it is a sign of a slow Redis and not of lost messages |
| `kibooz_chat_sockets_dropped_total` | `reason` | Sockets that the server closed, because a socket was `slow` to read or was `replaced` by a newer one of the account |
| `kibooz_chat_sockets_refused_total` | none | Sockets that were turned away because the copy was full or shutting down |
| `kibooz_push_messages_total` | `result` | Wake-up pushes, by `sent`, `failed`, `unregistered` (a token that Firebase dropped, which is deleted) and `dropped` (the queue was full or the server was stopping) |
| `kibooz_password_hash_busy_total` | none | Requests that got `429 RATE_LIMITED` because every password hashing slot was taken, which means the server needs more CPU or a higher `PASSWORD_HASH_CONCURRENCY` |
| `kibooz_job_runs_total` | `job`, `result` | Runs of a background job, with `result` `success` or `failure`. The jobs are `semester_check`, `file_cleanup`, `idempotency_purge`, `refresh_token_purge`, `keepalive_database` and `keepalive_redis` |
| `kibooz_job_last_success_timestamp_seconds` | `job` | Unix time of the last successful run, or of the moment the job started when it has not succeeded yet, so an alert can fire when a job has not succeeded for too long. A job that is switched off, such as the file cleanup without a bucket or the keepalive with `KEEPALIVE_SECONDS=0`, has no series |
| `go_sql_*` | `db_name` | The connection pool of the database, such as open, idle and waited-for connections |
| `go_*`, `process_*` | none | The Go runtime and the process, such as memory, goroutines and open files |

A request that answers `429` is counted in `kibooz_http_requests_total` as well. A request that panics is counted as a `500`. The metrics start from zero when the process restarts, so use `rate()` and `increase()` and not the raw counters. A Prometheus or an Alloy that scrapes the page sets its own `job` label on every series, which clashes with the `job` label of the two job metrics and turns it into `exported_job`. Scrape with `honor_labels` on, as the Alloy config of this repository does, to keep the name of the job in `job`.

## Logs

The API writes one JSON object per line to standard output, so `docker compose logs api` shows them and a log shipper can read the fields without a pattern. Every line has `time` (with the date and the zone), `level` and `msg`. Lines that the code writes with the `log` package, such as the startup lines and the failure of a job, are objects too, with their text in `msg`.

Every request has one line with `msg` `request`, written after the answer.

| Field | Meaning |
|:---|:---|
| `request_id` | The id that the client got in the `X-Request-Id` header of the response, which a user can quote from a failed request |
| `ip`, `method`, `path` | Who asked and for what. The query string is not logged |
| `route` | The path that the matching route was registered with, such as `/api/v1/wali/children/:studentId` |
| `status`, `latency_ms`, `bytes_out` | The answer, how long it took and the size of its body |
| `error_code`, `error_message` | On a failed request, the code and the message that the client got |
| `error_fields` | On a refused validation, the names of the fields that were refused. Their values are never logged |
| `retry_after` | On a `429`, the seconds that the client was told to wait |

A request that worked is logged at `INFO`, a `4xx` at `WARN` and a `5xx` at `ERROR`. A health check that answers `200` is logged at `DEBUG`, which is below the level that the API writes, so a monitor that asks every few seconds does not fill the log. A handler that panics writes a line with `msg` `panic recovered`, with the `panic` value, the `stack` and the same `request_id` as the request line, and the client gets a generic `500`. A failure that the client is not told about, such as the cause of an `INTERNAL_ERROR`, is logged with the request id in a separate line.

```sh
docker compose logs --since 1h api
docker compose logs --no-log-prefix -f api | jq -c 'select(.level == "ERROR")'
docker compose logs --no-log-prefix api | jq -c 'select(.status == 429)'
```

Startup prints the version and how long it took. A line saying that object storage is not configured means uploads are disabled on purpose. A `storage write probe failed` line means the bucket refused the health check write, which turns `/healthz` to `degraded`.

The health check uses a database connection of its own. The requests share a pool of 25 connections, and a check on that pool would wait behind them whenever all of them are busy, so a loaded API would look like a database that is down. A `503` from `/healthz` therefore means the database cannot be reached, and a pool that is full shows in the `go_sql_*` metrics instead.

## Uptime monitoring

Better Stack checks the `/healthz` route of the API and the documentation site every 3 minutes, from Europe, North America, Asia and Australia. A failure that lasts about 3 minutes opens an incident and sends an email and a push notification to the owner. The public page at <https://status.syafahadyan.com/> shows both monitors, and the badge of the documentation site at the top of the README reads from the monitor of the documentation site.

The free plan only looks at the status code. `/healthz` answers `503` when the database is down, so that raises an incident. It answers `200` with `degraded` when only Redis or the bucket is down, so that does not, and the log lines in the table below are the way to notice it. A deploy restarts the API for a few seconds, which is shorter than the confirmation period, so it does not page anyone. Pause the monitor in Better Stack before a longer planned stop.

## When something is wrong

| Symptom | Look at |
|:---|:---|
| `/healthz` answers `503` | The `kibooz-db` container and its health check. The API cannot serve without the database |
| Metrics stop arriving or `kibooz_job_last_success_timestamp_seconds` is old | That the API runs and that `METRICS_ADDRESS` is set and reachable from the scraper on the private network. A job that fails logs its own reason, see the lines in the logs section |
| `/healthz` says `degraded` | `redis` down means rate limits fall back to one process and the leaderboard is read from the database. `storage` down means the credentials or the bucket changed, and uploads will fail |
| The API container restarts in a loop | `docker compose logs api` shows the reason. A setting out of range, a missing required variable and a dirty migration all stop the start with a message, and so does a database that does not answer, which the API gives up on after 10 seconds. The 10 seconds cover opening the first connection. A start that takes longer is a long migration, the migration lock of another copy, which has no limit, or a database that stalls after it connected |
| Many `429` answers | The per user limit is `USER_LIMITER_MAX` per `LIMITER_EXPIRATION_SECONDS`. Login and registration are limited per email or device, see the [design notes](design.md) |
| Uploads fail with `STORAGE_UNAVAILABLE` | `S3_*` is not fully set |
| A video upload works but the video disappears | The `pending/` lifecycle rule is missing its prefix and also expires `videos/`. Fix the rule, the files that expired are gone |

## Roll back

Going back to an older image is safe only while no migration ran since that image. The migrator refuses to start when the database is at a version that the image does not know, so an older image cannot start on a newer schema.

1. If the new version added no migration, set `IMAGE_TAG` to the previous version and run `docker compose up -d`, or run `rolling-update.sh` with the previous version, which asks GitHub for the migrations of both versions when it can.
2. If it did, restore the database from a backup taken before the deploy and then start the older image. Alternatively, apply the down files of the new migrations with the golang-migrate command line and then start the older image. Down files drop the columns and tables the new version added, so the data in them is lost.

Nothing in this repository backs up the database. Take a dump before every deploy that carries a migration, for example with the command below, which reads the user and database name from the `db` container itself, and keep it somewhere off the host.

```sh
docker compose exec db sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > backup.sql
```

A volume from an older PostgreSQL major version does not start on PostgreSQL 18, see the [README](../README.md#production-deployment).

## Secrets

The `.env` file holds the database and Redis passwords, the JWT key, the bucket keys and the Sentry DSN. Keep it out of version control and readable only by the user that runs Docker. To rotate a secret, change it in `.env` and run `docker compose up -d`. Rotating `JWT_SECRET_KEY` makes the access tokens that are out stop working until their apps refresh, and rotating a database password also needs the same change inside PostgreSQL.
