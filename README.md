# Kibooz Backend

[![CI](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/ci.yaml)
[![Security](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/security.yaml)
[![Docker](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/docker.yaml)
[![Config lint](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/config.yaml)
[![Performance](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/perf.yaml)
[![DAST](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/dast.yaml)
[![Vale](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/vale.yaml/badge.svg?branch=main)](https://github.com/SyafaHadyan/kibooz-backend/actions/workflows/vale.yaml)
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

## Documentation

The API reference, with every route, field, limit, status code and error code, is at <https://docs.kibooz.syafahadyan.com> and is built from [`openapi.yaml`](openapi.yaml) (OpenAPI 3.0). Change the spec in the same pull request as an endpoint, because CI lints it and the end to end tests fail when a response or a route differs from it.

Cloudflare rebuilds the site on every push to `main` with `npm run build`, which uses the Redocly and Wrangler versions pinned in `package-lock.json` and the security headers in `docs/_headers`, and deploys it with `wrangler.jsonc`. The `package.json` only serves this site, so run `npm ci` once and then `npm run lint` or `npm run build` to try it locally. Dependabot keeps both tools current.

- [Design notes](docs/design.md) covers how accounts work, why Redis is optional and the business rules.
- [Testing](docs/testing.md) covers the test suites, coverage, fuzzing, the k6 performance tests and the ZAP scans.
- [CI/CD](docs/ci-cd.md) covers the workflows, the required checks and how releases are made.
- [Differences from the PRD](docs/prd-differences.md) lists where this implementation fills gaps in the PRD.

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

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report security problems privately as described in [SECURITY.md](SECURITY.md).

