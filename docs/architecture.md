# Architecture

Kibooz Backend is one Go program that serves a JSON REST API under `/api/v1`. It keeps its data in PostgreSQL and uses Redis and S3 compatible storage as optional helpers. The API contract is [openapi.yaml](../openapi.yaml), published at <https://docs.kibooz.syafahadyan.com>.

## Parts

| Part | What it does |
|:---|:---|
| API server | Fiber v3 HTTP server that handles routing, security headers, rate limits and errors |
| PostgreSQL | The only authority for users, sessions, classes, students, moods, scans and points. GORM with the pgx driver talks to it |
| Redis | Optional. It holds the shared rate limit counters, a short memory of consumed refresh tokens and the leaderboard cache |
| Object storage | Optional S3 compatible bucket for avatars and trash photos. Uploads are off when it is not configured |
| Migrations | SQL files embedded in the binary and applied with golang-migrate when the server starts |

Redis and the bucket can be missing or down without stopping the API, as described in the [design notes](design.md#redis-is-optional).

## Layers

Every feature is a folder under `internal/app` with the same three layers, so a request always travels the same way.

1. `interface/rest` reads the request, validates the body and writes the response. It holds no business rules.
2. `usecase` holds the business rules, such as the daily claim limit and who may see which student.
3. `repository` is the only code that talks to the database.

The features are `auth` for registration, login and tokens, `user` for the profile and avatar, `guru` for the teacher side, `wali` for the parent side and `trash` for photo scans and points. Shared code lives next to them.

| Folder | Purpose |
|:---|:---|
| `internal/domain` | Entities for the database and DTOs for requests and responses |
| `internal/apperror` | The error codes and their HTTP status, one list for the whole API |
| `internal/middleware` | Authentication and the per user rate limit |
| `internal/infra` | Fiber setup and limits, JWT, device tokens, validation, environment settings, Redis, S3 and the database with its migrations |
| `internal/imageutil` | Safe decoding and checking of uploaded images |
| `internal/bootstrap` | Builds everything from the settings and connects the layers |
| `internal/e2e` | End to end tests that also check every response against `openapi.yaml` |

## A request

1. The server adds the security headers and finds the route under `/api/v1`.
2. For a signed in route, the middleware checks the access token and then applies the per user rate limit. The public auth routes use a limit per account instead.
3. The handler validates the body and calls the use case.
4. The use case applies the rules and calls the repository, which reads or writes PostgreSQL in a transaction where the rules need one.
5. The response is wrapped in a common envelope. A failure becomes an error code from `internal/apperror`, never a raw error.

## Sessions

An access token is a short lived JWT. A refresh token is random, is stored only as a SHA-256 hash in the `refresh_tokens` table and works exactly once. The details and the reason Redis never decides validity are in the [design notes](design.md#redis-is-optional).

## Build and run

The image is a static Go binary on a distroless base that runs as a non-root user and listens on port 8080. It has a health check that calls `/healthz`. The [CI/CD page](ci-cd.md) describes how it is built, scanned, signed and released, and the [README](../README.md) has the settings and the Compose stack.
