# Kibooz Backend

[![CI](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml)
[![Security](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml)
[![Docker](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml)
[![Docker Hub mirror](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker-hub-mirror.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker-hub-mirror.yaml)
[![Config lint](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml)
[![Performance](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml)
[![DAST](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml)
[![DAST active](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast-active.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast-active.yaml)
[![Vale](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/vale.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/vale.yaml)
[![Fuzz](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/fuzz.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/fuzz.yaml)
[![Links](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/links.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/links.yaml)

[![Release](https://img.shields.io/github/v/release/SyafaHadyan/kibooz-backend?sort=semver)](https://github.com/SyafaHadyan/kibooz-backend/releases)
[![License](https://img.shields.io/github/license/SyafaHadyan/kibooz-backend)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/SyafaHadyan/kibooz-backend)](go.mod)

[![API](https://img.shields.io/badge/API-555555)](https://status.syafahadyan.com/)[![API uptime](https://incidents.betterstack.com/status-badges/v1/monitor/316aa.svg)](https://status.syafahadyan.com/)
[![Docs](https://img.shields.io/badge/Docs-555555)](https://status.syafahadyan.com/)[![Docs uptime](https://incidents.betterstack.com/status-badges/v1/monitor/316aj.svg)](https://status.syafahadyan.com/)

[![Docker pulls](https://img.shields.io/docker/pulls/syafa/kibooz-backend)](https://hub.docker.com/r/syafa/kibooz-backend)
[![Docker image size](https://img.shields.io/docker/image-size/syafa/kibooz-backend/latest)](https://hub.docker.com/r/syafa/kibooz-backend)

[![Last commit](https://img.shields.io/github/last-commit/SyafaHadyan/kibooz-backend)](https://github.com/SyafaHadyan/kibooz-backend/commits/main)
[![Conventional Commits](https://img.shields.io/badge/conventional%20commits-1.0.0-FE5196)](https://www.conventionalcommits.org)
[![Renovate](https://img.shields.io/badge/renovate-enabled-brightgreen?logo=renovatebot)](https://docs.renovatebot.com/)

[![codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend/branch/main/graph/badge.svg)](https://codecov.io/gh/SyafaHadyan/kibooz-backend)
[![Snyk](https://snyk.io/test/github/SyafaHadyan/kibooz-backend/badge.svg)](https://snyk.io/test/github/SyafaHadyan/kibooz-backend)
[![DeepSource](https://app.deepsource.com/gh/SyafaHadyan/kibooz-backend.svg/?label=active+issues&show_trend=true)](https://app.deepsource.com/gh/SyafaHadyan/kibooz-backend/)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/SyafaHadyan/kibooz-backend/badge)](https://scorecard.dev/viewer/?uri=github.com/SyafaHadyan/kibooz-backend)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/15283/badge)](https://www.bestpractices.dev/projects/15283)

REST API for Kibooz, the kindergarten app that lets teachers record children's moods, lets parents follow them, and rewards trash sorting with points on a class leaderboard. The contract follows `docs/prd_and_roadmap/02_BACKEND_API_AND_DATABASE.md` of the Android app repository.

## Stack

- Go with Fiber v3 and GORM
- PostgreSQL 18 for data, with SQL migrations embedded in the binary and applied on startup
- Redis 8 as an optional accelerator for replay rejection, shared rate limits and the leaderboard cache
- Any S3 compatible bucket (Cloudflare R2 by default) for avatars, trash photos and class videos
- JWT (HS256) access tokens with rotating opaque refresh tokens stored hashed in PostgreSQL, bcrypt password hashes

## Quick start

The API needs a PostgreSQL 18 reachable with the values in `.env`. Redis 8 is recommended but optional. Run it directly during development.

```sh
cp .env.example .env        # then edit the DB_*, REDIS_* and JWT_SECRET_KEY values
go run ./cmd/api
```

`GET /healthz` reports database and Redis status, answering 200 with `"status": "degraded"` when only Redis is down and 503 when the database is down. The `checks` object also lists `storage` as `ok`, `disabled` when the S3 settings are incomplete, or `down` when the bucket refuses a write, which also makes the status `degraded`. The storage check stores and deletes a small object under `healthcheck/` and reuses its answer for a minute, so a bucket whose credentials cannot write shows up here and not on the first upload. The `kibooz-backend healthcheck` command runs the same probe for container health checks and treats a degraded service as healthy.

## Production deployment

`compose.yml` is the production stack with the API, PostgreSQL and Redis, all restarting automatically. PostgreSQL and Redis are not published to the host. The API is published on `127.0.0.1:8080` only, so a reverse proxy with TLS on the same host has to sit in front of it, and set `TRUST_PROXY=true` so the access log shows real client IPs. The image is `syafa/kibooz-backend:latest`, which moves with every push to main, so set `IMAGE_TAG` in `.env`, for example to `0.6.0`, to stay on a release until you choose to update.

Every response carries security headers (`nosniff`, `X-Frame-Options: DENY`, a `default-src 'none'` content security policy, `no-referrer` and same-origin cross-origin policies). `Strict-Transport-Security` is not set by the API, so enable it on the proxy that ends TLS, where you know which subdomains are HTTPS only.

```sh
cp .env.example .env        # set DB_NAME, DB_USERNAME, DB_PASSWORD, REDIS_PASSWORD, JWT_SECRET_KEY and S3_*
docker compose pull
docker compose up -d
docker compose logs -f api
```

The database volume is mounted at `/var/lib/postgresql`, which is where the PostgreSQL 18 image keeps its data. A deployment that still has a volume from PostgreSQL 15 does not start on PostgreSQL 18 and needs a fresh volume or a migration first. When the data does not matter, stop the stack and remove the volume with `docker compose down -v`, and the API creates the schema again from its embedded migrations on the next start. To keep the data, dump it with `pg_dumpall` from the old container before the upgrade and load the dump into the new one, or run `pg_upgrade`.

## Configuration

Every value is an environment variable. A `.env` file is read when present. See `.env.example` for the full list. The server refuses to start when a limit, lifetime or port is out of range, for example a limit of 0, a cache time of 0 or a negative number of points, so a typo cannot switch a limiter off silently.

| Variable | Default | Purpose |
|:---|:---|:---|
| `APP_PORT` | `8080` | HTTP port |
| `METRICS_ADDRESS` | empty | Where the Prometheus endpoint listens, such as `:9100`, and empty switches it off. It is a listener of its own that serves only `GET /metrics`, so never publish its port or route it through the proxy, see [operations](docs/operations.md#metrics) |
| `APP_TIMEZONE` | `Asia/Jakarta` | School timezone used for "today" and the Monday to Friday chart |
| `BODY_LIMIT_MB` | `8` | Maximum request body size, which only the three photo uploads (the avatar, the trash photo and the photo of a chat message) can use, and `CHAT_PHOTO_MAX_MB` has to stay below it. Every other request is limited to 64 KB. The proxy needs the same limit, see [the design notes](docs/design.md#a-flood-limit-in-the-proxy) |
| `REQUEST_TIMEOUT_SECONDS` | `10` | Time a request may spend on the database, the cache and the storage before they are cancelled, it must be at least 1 |
| `CLASS_VIDEO_MAX_MB`, `VIDEO_UPLOAD_URL_SECONDS` | `100`, `900` | Largest video file a teacher may upload for a class, and how long the signed upload address works for the videos of a class and of the chat |
| `USER_LIMITER_MAX`, `LIMITER_EXPIRATION_SECONDS` | `120`, `60` | Requests each signed-in user may send per window, in seconds |
| `AUTH_LIMITER_MAX` | `10` | Requests per account and window for login, register, refresh and logout, and per user for deleting an account |
| `MOOD_BATCH_LIMITER_MAX` | `30` | Requests per user and window for `POST /guru/mood/log/batch`, on top of `USER_LIMITER_MAX`, at least 1 |
| `CLASS_CODE_LIMITER_MAX` | `60` | Requests per window that name a class code, counted for each route (register and adding a child) and shared by every caller of that route, at least 1. It limits guessing a join code, so raise it only when many parents sign up at the same time |
| `PASSWORD_HASH_CONCURRENCY`, `PASSWORD_HASH_WAIT_MILLISECONDS` | `0`, `1000` | How many passwords the server hashes or checks at the same time, and `0` means one for each CPU except one, with at least one. A request that finds every slot taken waits this many milliseconds, at least 1, and is then refused with `429` and a `Retry-After` |
| `PASSWORD_HASH_COST` | `10` | The bcrypt cost of new password hashes, from 4 to 14, and every step doubles the time of a hash. 10 is the bcrypt default and the lowest value that OWASP accepts, so a lower one is for tests only and logs a warning. Existing hashes keep their cost |
| `TRUST_PROXY`, `PROXY_HEADER` | `false`, `X-Forwarded-For` | Read the client IP from a proxy header, it is only used in the access log because no rate limit looks at the IP |
| `DB_*` | see example | PostgreSQL connection, `DB_NAME`, `DB_USERNAME` and `DB_PASSWORD` are required |
| `REDIS_*` | see example | Redis connection, the API starts and works without it. Set `REDIS_TLS=true` for hosted Redis that requires TLS. Set `REDIS_KEY_PREFIX`, for example `kibooz:`, when other applications use the same Redis, and every key of the API then starts with it. When `REDIS_USERNAME` is set, the bundled compose files also create that Redis user with `REDIS_PASSWORD` |
| `GOMEMLIMIT`, `REDIS_MAXMEMORY` | `200MiB`, `96mb` | Only read by the bundled compose files. A soft memory limit for the Go runtime and a cap for the bundled Redis that evicts only keys with an expiry |
| `API_MEMORY_LIMIT`, `REDIS_MEMORY_LIMIT` | `384m`, `192m` | Only read by the bundled compose files. A hard memory cap for the API and the Redis container, which has to stay above `GOMEMLIMIT` and `REDIS_MAXMEMORY`. The database has none on purpose |
| `LEADERBOARD_CACHE_SECONDS` | `300` | Leaderboard cache lifetime, it is also cleared whenever the ranking changes |
| `KEEPALIVE_SECONDS` | `60` | Seconds between a `SELECT 1` on the database and a `PING` on Redis, so hosted instances that pause when idle stay awake. `0` disables it |
| `JWT_SECRET_KEY` | required | At least 32 characters, and the sample value of `.env.example` is refused |
| `JWT_ACCESS_EXPIRED_MINUTES`, `JWT_REFRESH_EXPIRED_DAYS` | `15`, `30` | Token lifetimes |
| `REFRESH_REUSE_LEEWAY_SECONDS` | `10` | How long after its use a refresh token can be shown again and get a new token of the same session, for an app that lost the answer to its first call. Later the reuse ends the whole session. `0` switches it off and the highest value is `60` |
| `JWT_SESSION_MAX_DAYS` | `90` | The longest a sign in can last, however often its refresh token is used. After that the user signs in again |
| `DEVICE_TOKEN_TTL_DAYS` | `90` | How long the device token from login stays valid, at least 1. A device that sends it has its own login rate limit for the email |
| `FIREBASE_CREDENTIALS_FILE` | empty | The key file of the Firebase service account, mounted read only into the container. With it a closed app is woken when a chat message arrives, without it no push is sent. The wake-up carries ids and no text, see [ADR 0013](docs/adr/0013-chat-push-notifications.md) |
| `CHAT_MAX_SOCKETS`, `CHAT_MAX_SOCKETS_PER_USER` | `1000`, `5` | How many chat sockets one API copy holds and how many one account keeps open, see [Chat socket](docs/websocket.md) |
| `UPLOAD_CONCURRENCY` | `4` | How many photo uploads (avatar, scan claim, chat photo) one API copy handles at the same time, at least 1. The next one is refused with `429`, the code `RATE_LIMITED` and `Retry-After: 1`. The body of a request is already in memory when the check runs, so it bounds the work after the body arrives and not the bodies themselves |
| `S3_PRIVATE_BUCKET_NAME`, `CHAT_PHOTO_MAX_MB`, `CHAT_VIDEO_MAX_MB`, `CHAT_MEDIA_URL_SECONDS` | empty, `4`, `25`, `3600` | The private bucket of the chat photos and videos, which uses the account and keys of `S3_*` and is never public, the largest photo and video, and how long a signed read address works. Chat media is off while the bucket name is empty, and the bucket needs the same lifecycle rule on `pending/` |
| `S3_*` | empty | Object storage, uploads are disabled when it is not fully configured. `S3_PUBLIC_BUCKET_NAME` is the bucket that `S3_PUBLIC_URL` serves publicly, and the bucket needs a lifecycle rule that expires the `pending/` prefix after one day and never `videos/`, see [Architecture](docs/architecture.md) |
| `POINTS_ORGANIK`, `POINTS_ANORGANIK`, `POINTS_B3` | `10`, `15`, `0` | Reward per verified action |
| `TRASH_DAILY_LIMIT` | `5` | Claims per student per day |
| `IDEMPOTENCY_TTL_HOURS` | `168` | How long an `Idempotency-Key` is remembered, at least 1. A repeat inside this time gets the first response again instead of doing the work twice |
| `SENTRY_DSN` | empty | Where error reports go, and empty switches them off. A 5xx and a panic are sent with a scrubbed error text, the stack, the route pattern, the request id and the account id, and never with the request, see [Privacy](docs/privacy.md) |
| `SENTRY_ENVIRONMENT`, `SENTRY_SERVER_NAME` | `production`, empty | The environment and the host name that each error report carries |
| `SENTRY_EVENTS_PER_MINUTE` | `30` | The most error reports that leave in a minute, at least 1. The rest are dropped, so a crash loop cannot use up the quota |
| `SEMESTER_START_MONTHS` | `1,7` | Months in which a semester begins. On the first day of each, in `APP_TIMEZONE`, the leaderboard of every class starts from zero and the final standings are saved. The points of all semesters stay on the child |

## Documentation

The API reference, with every route, field, limit, status code and error code, is at <https://docs.kibooz.syafahadyan.com> and is built from [`openapi.yaml`](openapi.yaml) (OpenAPI 3.0). The API itself is served at <https://kibooz-api.syafahadyan.com/api/v1>. Change the spec in the same pull request as an endpoint, because CI lints it and the end to end tests fail when a response or a route differs from it.

Cloudflare rebuilds the site on every push to `main` with `npm run build`, which uses the Redocly and Wrangler versions pinned in `package-lock.json`, the security headers in `docs/_headers` and the icon in `docs/favicon.ico`, and deploys it with `wrangler.jsonc`. The `package.json` only serves this site, so run `npm ci` once and then `npm run lint` or `npm run build` to try it locally. Renovate keeps both tools current.

- [Architecture](docs/architecture.md) covers the parts, the layers of the code and the path of a request.
- [Design notes](docs/design.md) covers how accounts work, the rate limits, why Redis is optional and the business rules.
- [Data model](docs/data-model.md) covers the tables, the rules the database enforces, what deleting an account does and how the schema changes.
- [Security](docs/security.md) covers what is protected, the threats, the measures and what is not covered.
- [Decision records](docs/adr/README.md) explain why PostgreSQL decides and Redis helps, how idempotency keys work, why only merge commits are used and why only the release App creates tags.
- [Privacy](docs/privacy.md) covers which personal data is kept, who can read it, how long it stays and what deleting an account does.
- [Testing](docs/testing.md) covers the test suites, coverage, fuzzing, the k6 performance tests and the ZAP scans.
- [Pentest target](docs/pentest.md) explains how to start a throwaway stack on the private Tailscale network and attack it by hand.
- [CI/CD](docs/ci-cd.md) covers the workflows, the required checks and how releases are made.
- [Releasing](docs/releasing.md) covers how a version is chosen, what to check before the release pull request is merged, what the merge does and what to do afterwards.
- [Operations](docs/operations.md) covers deploying a version, migrations, background jobs, logs, what to check when something is wrong and how to roll back.
- [Monitoring](docs/monitoring.md) covers the metrics, the dashboards and the alerts in Grafana Cloud and how to switch them on.
- [Client guide](docs/client-guide.md) covers what an app has to do with the tokens, the device token and the rate limits.
- [Chat socket](docs/websocket.md) describes the frames of the WebSocket that pushes chat events, how to stay connected and the proxy settings.
- [Error codes](docs/errors.md) lists every `errorCode` with its status, what it means and what an app does about it.
- [Differences from the PRD](docs/prd-differences.md) lists where this implementation fills gaps in the PRD.
- [Questions and glossary](docs/faq.md) explains the Indonesian words in the API and answers the questions that come up most.

## Project layout

```
cmd/api/                  entry point
internal/app/<module>/    rest (Fiber handlers), usecase (rules), repository (GORM)
internal/domain/          entity (tables) and dto (payloads)
internal/infra/           env, db and migrations, redis, jwt, s3, fiber, validation
internal/middleware/      authentication and role checks
internal/e2e/             end to end tests
```

Modules are `auth`, `wali`, `guru`, `classroom` (learning videos and the class forum), `chat` (conversations of a teacher and a parent, and the room of each class), `push` (the tokens of the installed apps), `trash` (claims and leaderboard) and `user` (avatars).

## Contributing and security

Ask for help as described in [SUPPORT.md](SUPPORT.md). Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report security problems privately as described in [SECURITY.md](SECURITY.md). Report a barrier in the documentation or the API as described in [ACCESSIBILITY.md](ACCESSIBILITY.md).

