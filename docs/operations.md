# Operations

This page is for whoever runs the stack. It covers deploying a version, what the server does on its own, where to look when something is wrong and how to go back. The settings are listed in the [README](../README.md#configuration), and the reasons behind them are in the [design notes](design.md).

## What runs

The `compose.yml` file starts three containers on one host. `kibooz-api` is the image `syafa/kibooz-backend` and listens on `127.0.0.1:${APP_PORT}`, so only a reverse proxy on the same host reaches it. `kibooz-db` is PostgreSQL 18 and `kibooz-redis` is Redis 8. The API container has a read-only filesystem, drops every Linux capability and runs without new privileges. Each container keeps at most five log files of 10 MB.

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
   curl -s https://kibooz-api.syafahadyan.com/healthz
   ```

   The body has `status` `ok` and the `version` of the release. `degraded` means Redis is down or the bucket refuses writes, and the API still serves. `down` comes with a `503` and means the database does not answer.
5. Run the manual `Smoke` workflow from the Actions tab with `expected_version` set to the new version. It registers throwaway accounts, replays the main routes and removes the accounts again, see [CI/CD](ci-cd.md).

A restart stops accepting new requests and gives running ones 10 seconds to finish. Clients that lose a request in that window repeat it, and the routes that create something accept an `Idempotency-Key` so a repeat is safe.

## Migrations

The schema is built from SQL files embedded in the binary. The server applies every pending file with golang-migrate when it starts, before it listens, so there is no step to run by hand and a deploy that adds a migration needs nothing extra. The migrator takes a database lock, so two instances that start together do not apply the same file twice.

- Files are append only. A schema change is a new numbered file and never an edit of an old one, because other databases have already applied the old one. A test fails when a number is missing or used twice.
- Every migration has an up and a down file.
- A migration that fails halfway leaves the database marked dirty, and the next start stops with `Dirty database version`. Fix the cause, repair the data by hand if needed, and clear the mark with `migrate force <version>` from the golang-migrate command line before starting again.

## Settings that need care

| Setting | Why it matters |
|:---|:---|
| `JWT_SECRET_KEY` | Changing it makes every access token and device token stop verifying. Refresh tokens are random values stored in the database, so apps that refresh keep their sessions, but a device that sends an old device token falls back to the shared login bucket of its email. Keep it at least 32 characters and out of the repository |
| `APP_TIMEZONE` | Decides what "today" is, the Monday to Friday chart and the day the semester starts |
| `SEMESTER_START_MONTHS` | On the first day of each listed month the leaderboard of every class resets and the final standings are saved |
| `TRUST_PROXY`, `PROXY_HEADER` | Only the access log reads the client address, no limit does. Set them when a proxy sits in front, or every request is logged with the proxy address |
| `IDEMPOTENCY_TTL_HOURS` | How long a retried request gets the first answer again. Lowering it shortens the window in which an offline app can retry safely |
| `S3_*` | Uploads stay off until all of them are set. The bucket needs a lifecycle rule that expires `pending/` after one day and never `videos/` |

## Background work

The server runs four jobs by itself, and none of them needs a scheduler outside the process.

| Job | Every | What it does |
|:---|:---|:---|
| Semester check | 1 minute | Starts a new semester when the calendar says so, saves the final standings and clears the leaderboard cache |
| File cleanup | 10 minutes | Removes bucket files that a deleted account left behind and retries the ones that failed. Only runs when storage is configured |
| Idempotency purge | 1 hour | Deletes remembered keys older than `IDEMPOTENCY_TTL_HOURS` |
| Keepalive | `KEEPALIVE_SECONDS` | Sends `SELECT 1` to the database and `PING` to Redis, so hosted instances that pause when idle stay awake |

## Logs

The API writes plain text lines to standard output, so `docker compose logs api` shows them. A failed request is logged with its request id, the method and the error. Anything unexpected is logged as `unexpected error` with the path, and the response to the client is always a generic error code. The access log also has the request id, which a user can quote from a failed response header.

```sh
docker compose logs --since 1h api
docker compose logs -f api | grep "unexpected error"
```

Startup prints the version and how long it took. A line saying that object storage is not configured means uploads are disabled on purpose. A `storage write probe failed` line means the bucket refused the health check write, which turns `/healthz` to `degraded`.

## Uptime monitoring

Better Stack checks `https://kibooz-api.syafahadyan.com/healthz` and the documentation site every 3 minutes, from Europe, North America, Asia and Australia. A failure that lasts about 3 minutes opens an incident and sends an email and a push notification to the owner. The public page at <https://status.syafahadyan.com/> shows both monitors, and the badges at the top of the README read from them.

The free plan only looks at the status code. `/healthz` answers `503` when the database is down, so that raises an incident. It answers `200` with `degraded` when only Redis or the bucket is down, so that does not, and the log lines in the table below are the way to notice it. A deploy restarts the API for a few seconds, which is shorter than the confirmation period, so it does not page anyone. Pause the monitor in Better Stack before a longer planned stop.

## When something is wrong

| Symptom | Look at |
|:---|:---|
| `/healthz` answers `503` | The `kibooz-db` container and its health check. The API cannot serve without the database |
| `/healthz` says `degraded` | `redis` down means rate limits fall back to one process and the leaderboard is read from the database. `storage` down means the credentials or the bucket changed, and uploads will fail |
| The API container restarts in a loop | `docker compose logs api` shows the reason. A setting out of range, a missing required variable and a dirty migration all stop the start with a message |
| Many `429` answers | The per user limit is `USER_LIMITER_MAX` per `LIMITER_EXPIRATION_SECONDS`. Login and registration are limited per email or device, see the [design notes](design.md) |
| Uploads fail with `STORAGE_UNAVAILABLE` | `S3_*` is not fully set |
| A video upload works but the video disappears | The `pending/` lifecycle rule is missing its prefix and also expires `videos/`. Fix the rule, the files that expired are gone |

## Roll back

Going back to an older image is safe only while no migration ran since that image. The migrator refuses to start when the database is at a version that the image does not know, so an older image cannot start on a newer schema.

1. If the new version added no migration, set `IMAGE_TAG` to the previous version and run `docker compose up -d`.
2. If it did, restore the database from a backup taken before the deploy and then start the older image. Alternatively, apply the down files of the new migrations with the golang-migrate command line and then start the older image. Down files drop the columns and tables the new version added, so the data in them is lost.

Nothing in this repository backs up the database. Take a dump before every deploy that carries a migration, for example with the command below, which reads the user and database name from the `db` container itself, and keep it somewhere off the host.

```sh
docker compose exec db sh -c 'pg_dump -U "$POSTGRES_USER" "$POSTGRES_DB"' > backup.sql
```

A volume from an older PostgreSQL major version does not start on PostgreSQL 18, see the [README](../README.md#production-deployment).

## Secrets

The `.env` file holds the database and Redis passwords, the JWT key and the bucket keys. Keep it out of version control and readable only by the user that runs Docker. To rotate a secret, change it in `.env` and run `docker compose up -d`. Rotating `JWT_SECRET_KEY` makes the access tokens that are out stop working until their apps refresh, and rotating a database password also needs the same change inside PostgreSQL.
