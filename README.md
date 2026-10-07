# Kibooz Backend

[![CI](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml)
[![Security](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml)
[![Docker](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml)
[![Config lint](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml)
[![Performance](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml)
[![DAST](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml)
[![codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend/branch/main/graph/badge.svg)](https://codecov.io/gh/SyafaHadyan/kibooz-backend)

[![Release](https://img.shields.io/github/v/release/SyafaHadyan/kibooz-backend?sort=semver)](https://github.com/SyafaHadyan/kibooz-backend/releases)
[![License](https://img.shields.io/github/license/SyafaHadyan/kibooz-backend)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/SyafaHadyan/kibooz-backend)](go.mod)

[![Docker pulls](https://img.shields.io/docker/pulls/syafa/kibooz-backend)](https://hub.docker.com/r/syafa/kibooz-backend)
[![Docker image size](https://img.shields.io/docker/image-size/syafa/kibooz-backend/latest)](https://hub.docker.com/r/syafa/kibooz-backend)

[![Last commit](https://img.shields.io/github/last-commit/SyafaHadyan/kibooz-backend)](https://github.com/SyafaHadyan/kibooz-backend/commits/main)
[![Conventional Commits](https://img.shields.io/badge/conventional%20commits-1.0.0-FE5196)](https://www.conventionalcommits.org)

[![DeepSource](https://app.deepsource.com/gh/SyafaHadyan/kibooz-backend.svg/?label=active+issues&show_trend=true)](https://app.deepsource.com/gh/SyafaHadyan/kibooz-backend/)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/SyafaHadyan/kibooz-backend/badge)](https://scorecard.dev/viewer/?uri=github.com/SyafaHadyan/kibooz-backend)

REST API for Kibooz, the kindergarten app that lets teachers record children's moods, lets parents follow them, and rewards trash sorting with points on a class leaderboard. The contract follows `docs/prd_and_roadmap/02_BACKEND_API_AND_DATABASE.md` of the Android app repository.

## Stack

- Go with Fiber v3 and GORM
- PostgreSQL 15 for data, with SQL migrations embedded in the binary and applied on startup
- Redis 7 as an optional accelerator for replay rejection, shared rate limits and the leaderboard cache
- Any S3 compatible bucket (Cloudflare R2 by default) for avatars and trash photos
- JWT (HS256) access tokens with rotating opaque refresh tokens stored hashed in PostgreSQL, bcrypt password hashes

## Quick start

The API needs a PostgreSQL 15 reachable with the values in `.env`. Redis 7 is recommended but optional. Run it directly during development.

```sh
cp .env.example .env        # then edit the DB_*, REDIS_* and JWT_SECRET_KEY values
go run ./cmd/api
```

`GET /healthz` reports database and Redis status, answering 200 with `"status": "degraded"` when only Redis is down and 503 when the database is down. The `checks` object also lists `storage` as `ok` or `disabled`, which only reflects whether the S3 settings are complete and never changes the status. The `kibooz-backend healthcheck` command runs the same probe for container health checks and treats a degraded service as healthy.

## Production deployment

`compose.yml` is the production stack with the API, PostgreSQL and Redis, all restarting automatically. PostgreSQL and Redis are not published to the host. The API is published on port 8080, so put a reverse proxy with TLS in front of it and set `TRUST_PROXY=true` so rate limiting sees real client IPs.

Every response carries security headers (`nosniff`, `X-Frame-Options: DENY`, a `default-src 'none'` content security policy, `no-referrer` and same-origin cross-origin policies). `Strict-Transport-Security` is not set by the API, so enable it on the proxy that ends TLS, where you know which subdomains are HTTPS only.

```sh
cp .env.example .env        # set DB_NAME, DB_USERNAME, DB_PASSWORD, REDIS_PASSWORD, JWT_SECRET_KEY and S3_*
docker compose pull
docker compose up -d
docker compose logs -f api
```

## Configuration

Every value is an environment variable. A `.env` file is read when present. See `.env.example` for the full list.

| Variable | Default | Purpose |
|:---|:---|:---|
| `APP_PORT` | `8080` | HTTP port |
| `APP_TIMEZONE` | `Asia/Jakarta` | School timezone used for "today" and the Monday to Friday chart |
| `BODY_LIMIT_MB` | `8` | Maximum request body size |
| `LIMITER_MAX`, `LIMITER_EXPIRATION_SECONDS` | `90`, `60` | Global rate limit per IP |
| `AUTH_LIMITER_MAX` | `10` | Rate limit per IP for `/auth/*` inside the same window |
| `TRUST_PROXY`, `PROXY_HEADER` | `false`, `X-Forwarded-For` | Read the client IP from a proxy header |
| `DB_*` | see example | PostgreSQL connection, `DB_NAME`, `DB_USERNAME` and `DB_PASSWORD` are required |
| `REDIS_*` | see example | Redis connection, the API starts and works without it. Set `REDIS_TLS=true` for hosted Redis that requires TLS. When `REDIS_USERNAME` is set, the bundled compose files also create that Redis user with `REDIS_PASSWORD` |
| `GOMEMLIMIT`, `REDIS_MAXMEMORY` | `200MiB`, `96mb` | Only read by the bundled compose files. A soft memory limit for the Go runtime and a cap for the bundled Redis that evicts only keys with an expiry |
| `LEADERBOARD_CACHE_SECONDS` | `300` | Leaderboard cache lifetime, it is also cleared whenever the ranking changes |
| `KEEPALIVE_SECONDS` | `60` | Seconds between a `SELECT 1` on the database and a `PING` on Redis, so hosted instances that pause when idle stay awake. `0` disables it |
| `JWT_SECRET_KEY` | required | At least 32 characters |
| `JWT_ACCESS_EXPIRED_MINUTES`, `JWT_REFRESH_EXPIRED_DAYS` | `60`, `30` | Token lifetimes |
| `S3_*` | empty | Object storage, uploads are disabled when it is not fully configured |
| `POINTS_ORGANIK`, `POINTS_ANORGANIK`, `POINTS_B3` | `10`, `15`, `0` | Reward per verified action |
| `TRASH_DAILY_LIMIT` | `5` | Claims per student per day |

## API

All routes live under `/api/v1`. Responses use one envelope.

```json
{ "success": true, "message": "Login successful", "data": {} }
{ "success": false, "message": "Email or password does not match the selected role", "errorCode": "AUTH_INVALID_CREDENTIALS" }
```

Validation failures use `VALIDATION_ERROR` and add a `details` object with one message per field. Protected routes need the access token as a bearer token in the `Authorization` header.

The full contract, with every field, limit, status code and error code, is in [`openapi.yaml`](openapi.yaml) (OpenAPI 3.0). Change it in the same pull request as an endpoint, because CI lints it. Any OpenAPI viewer can browse it.

| Method and path | Role | Purpose |
|:---|:---|:---|
| `POST /auth/register` | public | Create a GURU with a new class, or a WALI with a child |
| `POST /auth/login` | public | Login with email, password and role |
| `POST /auth/refresh-token` | public | Exchange a refresh token for a new pair, the old one stops working |
| `POST /auth/logout` | public | Revoke a refresh token |
| `GET /wali/dashboard` | WALI | Child, today's mood, points summary and recommended guidance, optional `studentId` |
| `POST /wali/guidance/apply` | WALI | Tell the teacher a guidance was applied at home |
| `GET /guru/dashboard` | GURU | Class overview and today's mood distribution, optional `classId` |
| `POST /guru/mood/log` | GURU | Record a child's mood |
| `GET /guru/mood/analytics` | GURU | Donut summary and Monday to Friday trend, `range` is `weekly` or `monthly` |
| `POST /trash/scan-claim` | WALI | Claim points for sorted trash |
| `GET /leaderboard` | GURU, WALI | Podium and ranking of a class, optional `classId` |
| `POST /users/avatar` | GURU, WALI | Multipart upload, field `file` and optional `studentId` for a child |
| `DELETE /users/me` | GURU, WALI | Delete the own account after confirming the password in the JSON body, a parent's children are deleted with it |

### Accounts

There is no seed data. Accounts come from `POST /auth/register`.

- A GURU registers together with a new class (`class.name` is required) and becomes its teacher. The class gets a six character `joinCode` that the dashboard returns.
- A WALI registers with the teacher's `classCode` and the child's `student.nisn` and `student.fullName`.
- A GURU cannot join an existing class, so knowing a class code never gives access to its children's data.
- ADMIN exists in the role enum but cannot be registered.

### Redis is optional

PostgreSQL is the only authority for sessions. Refresh tokens live in the `refresh_tokens` table as SHA-256 hashes, and a token is consumed by a single atomic `DELETE ... RETURNING`, so it can be used exactly once even under concurrent requests.

Redis never decides that a token is valid. It only remembers consumed tokens for a minute so a quick replay is rejected without a database call, and it holds the leaderboard cache and the shared rate limit counters. This also means a stale or restored Redis cannot revive a revoked token.

When Redis is unreachable the API logs it once and keeps serving. Calls to Redis are skipped for a few seconds after a failure so there is no added latency, rate limiting falls back to per-process counters, the leaderboard is read from PostgreSQL, and `/healthz` reports `degraded`. When Redis returns, it is picked up again automatically.

### Business rules

- A student can claim at most `TRASH_DAILY_LIMIT` times per local day. The check, the point update and the re-ranking run in one transaction guarded by a per class advisory lock, so parallel requests cannot exceed the limit.
- Ranking is points descending, then name, then id. `students.rank_position` is kept in sync on every claim and registration.
- Only the latest mood of a student per local day counts in dashboards and charts.
- A class whose teachers were all deleted keeps its code, but registering a child with it fails with `CLASS_NO_ACTIVE_TEACHER`.
- Deleting an account is a soft delete. The user, their profile and a parent's children get a `deleted_at` time and disappear from every query, login and refresh token, and the class ranking is renumbered. Moods, scans and guidance records stay in the database. Email, NIP and NISN are only unique among active rows, so they can be registered again, and permanent removal is not automated.
- A teacher can only record and read moods of classes they teach. A parent can only read their own children. Cross class access returns 403 or 404.
- Photos are decoded, size checked and type checked by content, not by file name. Raw face photos are never accepted because mood detection runs on the device.

## Project layout

```
cmd/api/                  entry point
internal/app/<module>/    rest (Fiber handlers), usecase (rules), repository (GORM)
internal/domain/          entity (tables) and dto (payloads)
internal/infra/           env, db and migrations, redis, jwt, s3, fiber, validation
internal/middleware/      authentication and role checks
internal/e2e/             end to end tests
```

Modules are `auth`, `wali`, `guru`, `trash` (claims and leaderboard) and `user` (avatars).

## Testing

```sh
go test ./...                 # unit tests, end to end tests are skipped
```

The end to end suite drives the real HTTP stack against PostgreSQL and Redis, with an in-process mock for object storage. Point it at a disposable PostgreSQL and Redis and enable it explicitly, because the tests create data.

```sh
E2E_ENABLED=true DB_NAME=kibooz DB_USERNAME=kibooz DB_PASSWORD=... go test ./... -race -count=1
```

Run the linter with `golangci-lint run` (configuration in `.golangci.yml`).

CI uploads the coverage of the unit and end to end tests to [Codecov](https://codecov.io/gh/SyafaHadyan/kibooz-backend), which comments on pull requests that change it. `codecov/patch` is a required check, so new code needs coverage. Codecov posts it a few minutes after CI finishes, so a merge has to wait for it. `codecov/project` is not required. Dependabot pull requests upload too, using a `CODECOV_TOKEN` stored in the Dependabot secrets. The thresholds are in `codecov.yml`.

The code that takes untrusted input has fuzz tests, namely image decoding, access token validation and request body validation. A normal `go test` runs their seed cases. To search for new failing inputs, fuzz one target at a time.

```sh
go test ./internal/imageutil/ -run '^$' -fuzz FuzzDecodeBase64 -fuzztime 30s
```

### Performance tests

The `k6/` folder holds the load scripts. `smoke.js` is a short check that runs on every pull request, and `load.js` holds a steady arrival rate below the ceiling to measure how the API behaves with a whole class claiming points at once, and `stress.js` ramps past the ceiling on demand to find where the API slows down. `spike.js` sends a sudden burst above the ceiling and checks that the API recovers afterwards, and `soak.js` holds a steady load for 30 minutes to catch leaks, so its summary also lists the memory of every container at the start and at the end. All of them create their own accounts, so they only need a running API. The default rate limits and the daily claim limit would answer with errors during a load test, so the workflow raises them in its generated `.env`.

```sh
# set LIMITER_MAX, AUTH_LIMITER_MAX and TRASH_DAILY_LIMIT to large values in .env first
docker compose -f compose.yml -f compose.ci.yml up -d --build --wait
k6 run k6/smoke.js
```

The latency limits in the scripts are placeholders that were set from the first runs on a GitHub runner, so raise them when you move to slower hardware. Never point the scripts at a deployment you do not own, because they create accounts and write data.

### Security scan with ZAP

The `dast.yaml` workflow starts the compose stack and runs the ZAP baseline scan, which is a passive scan that only reads responses and never attacks. The API is plain JSON without links, so the spider finds almost nothing by itself. `.zap/seed.sh` therefore registers a teacher and a parent with throwaway credentials and `.zap/hook.py` replays a dozen requests through ZAP, including authenticated, rejected and unknown routes. Every warning fails the build. A rule that does not apply can be set to `IGNORE` with a reason in `.zap/rules.tsv`. The report is uploaded as the `zap-reports` artifact.

The scan is passive, so it checks headers and information leaks but does not try injections. The ZAP image is pinned by digest and has to be bumped by hand in the workflow, because Dependabot does not read images inside shell steps.

## CI/CD

| Workflow | Trigger | What it does |
|:---|:---|:---|
| `ci.yaml` | push to main, pull requests | gofmt, tidy check, vet, golangci-lint, build, race tests with PostgreSQL and Redis services, coverage upload to Codecov |
| `security.yaml` | push, pull requests, daily at 03:00 WIB | CodeQL, govulncheck, dependency review, gitleaks secret scan, Trivy filesystem scan, OSSF Scorecard (not on pull requests) |
| `config.yaml` | push to main, pull requests | actionlint and zizmor for the workflows, hadolint for the Dockerfile, Redocly lint for `openapi.yaml` |
| `perf.yaml` | pull requests, daily at 04:00 WIB, manual | Builds the compose stack from the pull request with a memory cap on every container, runs k6 (a 30 second smoke test on pull requests, a steady load test nightly and on demand, and a stress, spike or soak test on demand) and fails when a container was killed or restarted |
| `dast.yaml` | pull requests, weekly on Monday, manual | Builds the compose stack from the pull request, registers a teacher and a parent, replays a dozen API requests through the ZAP baseline scan and fails on any warning. The reports are uploaded as an artifact |
| `docker.yaml` | push to main, tags, pull requests | Builds the image, pushes it to GitHub Container Registry and to Docker Hub when configured, scans with Trivy, attaches an SBOM and signs with cosign |
| `release-please.yaml` | push to main | Keeps a release PR with the next version and `CHANGELOG.md`, merging it creates the tag and the GitHub release |

Image tags are `latest` for main, `sha-<commit>` for every build, `pr-<number>` for pull requests, and `1.2.3`, `1.2` and `1` for version tags. The image is always published to GitHub Container Registry as `ghcr.io/syafahadyan/kibooz-backend` with the built-in token, so it needs no setup. Docker Hub is optional and needs the repository variable `DOCKERHUB_USERNAME` and the secret `DOCKERHUB_TOKEN`. Pull requests from forks and Dependabot only build the image. All actions are pinned to commit SHAs and kept current by Dependabot, which waits 7 days after a new release before proposing it.

### Required checks

A pull request can only be merged into `main` when these checks pass. The repository ruleset pins every check to the app that reports it, so another app cannot post a check with the same name. Add a new check to the ruleset in the same change that introduces it.

| Check | Reported by |
|:---|:---|
| Lint | GitHub Actions |
| Unit and end to end tests | GitHub Actions |
| Build, scan and push image | GitHub Actions |
| Go vulnerability check | GitHub Actions |
| Dependency review | GitHub Actions |
| Secret scan (gitleaks) | GitHub Actions |
| ZAP baseline scan | GitHub Actions |
| Trivy filesystem scan | GitHub Actions |
| Actionlint, Zizmor, Hadolint and OpenAPI lint | GitHub Actions |
| CodeQL and Trivy code scanning results | GitHub Advanced Security |
| DeepSource Docker, Go, SQL and Secrets | DeepSource |
| codecov/patch | Codecov |
| security/snyk | Snyk, a commit status that cannot be pinned to an app |

OSSF Scorecard is not required because it only runs on `main`, and the k6 performance test is not required yet while its limits are being calibrated. DeepSource and Snyk are GitHub apps and are not workflows in this repository.

### Releases

Versions are decided by [release-please](https://github.com/googleapis/release-please) from the Conventional Commit messages that reach `main`. `fix` bumps the patch version, `feat` bumps the minor version, and `feat!` or a `BREAKING CHANGE` footer bumps the major version. While the version is below 1.0.0 a breaking change only bumps the minor version.

1. Merge pull requests into `main` with a Conventional Commit title.
2. release-please opens or updates a pull request named like `chore(main) release 0.2.0` with the new version and changelog.
3. Merge that pull request when you want to release. This creates the `v0.2.0` tag and GitHub release, and the tag makes `docker.yaml` push the `0.2.0`, `0.2` and `0` image tags.

For the release pull request to run CI and for the tag to trigger the Docker workflow, add a repository secret named `RELEASE_PLEASE_TOKEN` holding a personal access token with contents and pull requests write access. GitHub does not start workflows for events created with the default token. Also allow GitHub Actions to create pull requests in the repository settings.

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report security problems privately as described in [SECURITY.md](SECURITY.md).

## Differences from the PRD

The PRD leaves a few gaps that this implementation fills.

- `class_teachers` (present in the ERD but not the DDL), `classes.join_code` and `guidance_applications` were added to the schema.
- `gen_random_uuid()` replaces `uuid_generate_v4()`, so no extension is needed.
- `POST /auth/register`, `POST /auth/refresh-token`, `POST /auth/logout` and `POST /users/avatar` were added, and `classOverview` also returns `joinCode`.
- The PRD defines points only for organic and inorganic trash, so B3 is recorded with `POINTS_B3` (0 by default).
- BR-04 (confirm AI results below 75% confidence) is a client dialog, so the server stores whatever confidence the confirmed result carries.
- `averageHappyScore` is the share of that day's students whose latest mood is SENANG.
- Resetting rankings at a new semester (BR-03) is not automated yet.
