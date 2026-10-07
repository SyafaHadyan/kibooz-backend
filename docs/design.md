# Design notes

How accounts, sessions, Redis and the business rules behave. The routes, fields and error codes are in the API reference at <https://docs.kibooz.syafahadyan.com>.

## Accounts

There is no seed data. Accounts come from `POST /auth/register`.

- A GURU registers together with a new class (`class.name` is required) and becomes its teacher. The class gets a six character `joinCode` that the dashboard returns.
- A WALI registers with the teacher's `classCode` and the child's `student.nisn` and `student.fullName`.
- A GURU cannot join an existing class, so knowing a class code never gives access to its children's data.
- ADMIN exists in the role enum but cannot be registered.

## Redis is optional

PostgreSQL is the only authority for sessions. Refresh tokens live in the `refresh_tokens` table as SHA-256 hashes, and a token is consumed by a single atomic `DELETE ... RETURNING`, so it can be used exactly once even under concurrent requests.

Redis never decides that a token is valid. It only remembers consumed tokens for a minute so a quick replay is rejected without a database call, and it holds the leaderboard cache and the shared rate limit counters. This also means a stale or restored Redis cannot revive a revoked token.

When Redis is unreachable the API logs it once and keeps serving. Calls to Redis are skipped for a few seconds after a failure so there is no added latency, rate limiting falls back to per-process counters, the leaderboard is read from PostgreSQL, and `/healthz` reports `degraded`. When Redis returns, it is picked up again automatically.

## Business rules

- A student can claim at most `TRASH_DAILY_LIMIT` times per local day. The check, the point update and the re-ranking run in one transaction guarded by a per class advisory lock, so parallel requests cannot exceed the limit.
- Ranking is points descending, then name, then id. `students.rank_position` is kept in sync on every claim and registration.
- Only the latest mood of a student per local day counts in dashboards and charts.
- A class whose teachers were all deleted keeps its code, but registering a child with it fails with `CLASS_NO_ACTIVE_TEACHER`.
- Deleting an account is a soft delete. The user, their profile and a parent's children get a `deleted_at` time and disappear from every query, login and refresh token, and the class ranking is renumbered. Moods, scans and guidance records stay in the database. Email, NIP and NISN are only unique among active rows, so they can be registered again, and permanent removal is not automated.
- A teacher can only record and read moods of classes they teach. A parent can only read their own children. Cross class access returns 403 or 404.
- Photos are decoded, size checked and type checked by content, not by file name. Raw face photos are never accepted because mood detection runs on the device.
