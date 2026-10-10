# Design notes

How accounts, sessions, rate limits, Redis and the business rules behave. The routes, fields and error codes are in the API reference at <https://docs.kibooz.syafahadyan.com>.

## Accounts

There is no seed data. Accounts come from `POST /auth/register`.

- A GURU registers together with a new class (`class.name` is required) and becomes its teacher. The class gets a six character `joinCode` that the dashboard returns.
- A WALI registers with the teacher's `classCode` and the child's `student.nisn` and `student.fullName`.
- A GURU cannot join an existing class, so knowing a class code never gives access to its children's data.
- ADMIN exists in the role enum but cannot be registered.

## Rate limits

No rate limit looks at the IP address. A school network or an ISP puts many people behind one public address, so an address says little about who is asking and one noisy person would throttle everyone else on it. The `TRUST_PROXY` setting only decides which address the access log shows. Every limit is a sliding window of `LIMITER_EXPIRATION_SECONDS` and answers `429` with `RATE_LIMITED` and a `Retry-After` header. The daily limit of trash claims is not one of these limits, it answers `429` with `TRASH_DAILY_LIMIT_REACHED` and no `Retry-After` header.

| Where | Counted per | Allowance |
|:---|:---|:---|
| Every route that needs a signed-in user | user id | `USER_LIMITER_MAX`, 120 by default |
| Deleting an account, which confirms the password | user id | `AUTH_LIMITER_MAX`, 10 by default |
| Login and register | email, or the device when the login carries a valid device token | `AUTH_LIMITER_MAX`, 10 by default, separately for each route |
| Refresh token and logout | refresh token | `AUTH_LIMITER_MAX`, 10 by default, separately for each route |
| `/healthz` | nothing | not limited |

Each route is limited by the one field it actually reads, so adding a second field to the body, such as an email on a refresh request, does not give a caller a new budget. The email is trimmed and lower-cased the same way the sign in does it, so changing the letter case does not either. The identifiers are hashed before they become a storage key, so no email or token is kept in Redis in clear. A request that names no account, such as an empty or malformed body, is not counted, because it is rejected before it touches the database.

The counters live in Redis when it is reachable, so every instance of the API shares them. A request is counted with atomic commands in one transaction, `INCR` on the counter of the current window and a read of the previous one, and no instance reads a count to write it back, so instances that count at the same moment never lose a request. Windows are the consecutive stretches of `LIMITER_EXPIRATION_SECONDS` since the Unix epoch, so the instances agree on where a window starts as long as their clocks are in sync. The sliding window counts the previous window for the part of it that the last window still covers, rounded up. Only the count of each window is kept and not the time of each request, so this is an estimate that assumes the requests of the previous window were spread evenly, and rounding up only keeps truncation from losing a whole request at the start of a window. No lock is held while the API waits for Redis, so a Redis that is far away slows each request by one round trip and does not queue the requests behind one another. Without Redis each process counts for itself. When other applications use the same Redis, set `REDIS_KEY_PREFIX` and every key of the API starts with it, and the e2e tests of this repository need a `REDIS_DATABASE` of their own because they empty it.

### Trusted devices

Anyone who knows an email can use up its shared login budget, so a limit per email alone would let a stranger keep the owner from signing in. To prevent that, registration and login return a `deviceToken` that the app keeps and sends in the next login request. A login that carries a valid token for its email is counted in a bucket of that device, and every other login is counted in the shared bucket of the email. A stranger can only fill the shared bucket, so the devices that have signed in before keep working.

The token is not a credential and never replaces the password. It is signed with a key derived from `JWT_SECRET_KEY`, bound to the email, valid for `DEVICE_TOKEN_TTL_DAYS` (90 by default) and renewed by every login, and a token refresh does not return one. A forged, expired or foreign token is rejected by the signature alone, without a database lookup, and the request falls back to the shared bucket. A stolen token only gives the thief the same `AUTH_LIMITER_MAX` attempts per window in the bucket of that one device, and the password is still needed.

A new device, or one that cleared its data, has no token and uses the shared bucket. While someone is using it up, that device cannot sign in until the window passes.

Two things follow from not using the address. Guessing the password of one account is stopped from any number of addresses, and anyone can still send `AUTH_LIMITER_MAX` failed logins for someone else's email to block new devices for that person for a window. A flood that names a new account in every request, such as thousands of sign ups with random emails, is not stopped by the API at all, so keep a limit in front of it in the reverse proxy or the CDN.

### A flood limit in the proxy

The proxy limit is a ceiling for floods and not a throttle for people. It still counts by address, so once an address goes over the ceiling the proxy rejects everyone who shares it, including the users who behave. That is why the numbers have to sit far above what one school network sends, and the API already limits every user and account on its own. This nginx example allows each address 30 requests a second with a burst of 60 on the four public auth routes and leaves every other route to the API.

```nginx
# http block
limit_req_zone $binary_remote_addr zone=kibooz_auth:10m rate=30r/s;
limit_req_status 429;

# server block
location ~ ^/api/v1/auth/(register|login|refresh-token|logout)$ {
    limit_req zone=kibooz_auth burst=60 nodelay;
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}

location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

If a CDN or load balancer sits in front of nginx, restore the visitor address first with `set_real_ip_from` and `real_ip_header`. Without that, every request arrives from the address of the CDN, all visitors share one budget and the limit blocks everyone at once. A request that nginx limits gets a plain `429` without the `RATE_LIMITED` body, so the apps treat any `429` as a signal to wait. A CDN rule on the same four routes works the same way and stops the flood before it reaches your server.

## Redis is optional

PostgreSQL is the only authority for sessions. Refresh tokens live in the `refresh_tokens` table as SHA-256 hashes, and a token is used by a single atomic `UPDATE ... RETURNING` that sets `used_at`, so it can be used exactly once even under concurrent requests. Every sign in has a `family_id` that its tokens share, and the used rows stay for a week. A used token that is shown again more than a minute after its use, which is the time Redis flags it for, is taken for a theft. The whole family is deleted, the owner is logged and both the thief and the owner have to sign in again. Within that minute the token is only refused, because a client that lost the answer retries with the same token. `session_started_at` is copied to every token of the family and caps the expiry of the next token at `JWT_SESSION_MAX_DAYS`, 90 by default, so refreshing cannot keep a session alive for ever. Signing out deletes the whole family.

Redis never decides that a token is valid. It only remembers consumed tokens for a minute so a quick replay is rejected without a database call, and it holds the leaderboard cache and the shared rate limit counters. This also means a stale or restored Redis cannot revive a revoked token.

When Redis is unreachable the API logs it once and keeps serving. Calls to Redis are skipped for a few seconds after a failure so there is no added latency, rate limiting falls back to per-process counters, the leaderboard is read from PostgreSQL, and `/healthz` reports `degraded`. When Redis returns, it is picked up again automatically.

## Business rules

- A student can claim at most `TRASH_DAILY_LIMIT` times per local day. The check, the point update and the re-ranking run in one transaction guarded by a per class advisory lock, so parallel requests cannot exceed the limit.
- Ranking is points descending, then name, then id. `students.rank_position` is kept in sync on every claim and registration.
- Only the latest mood of a student per local day counts in dashboards and charts.
- A class whose teachers were all deleted keeps its code, but registering a child with it fails with `CLASS_NO_ACTIVE_TEACHER`.
- Deleting an account is a soft delete. The user, their profile and a parent's children get a `deleted_at` time and disappear from every query, login and refresh token, and the class ranking is renumbered. Moods, scans and guidance records stay in the database. The avatar of the user, the avatars of the children and the photos of their trash scans are deleted from the bucket and their addresses are cleared, so no photo of a deleted account is kept. A file that cannot be deleted at that moment is logged and stays in the bucket. Email, NIP and NISN are only unique among active rows, so they can be registered again, and permanent removal is not automated.
- A teacher can only record and read moods of classes they teach. A parent can only read their own children. Cross class access returns 403 or 404.
- Photos are size checked and type checked by their content, not by file name, and they are stored without their metadata. The EXIF, XMP, IPTC and comment details, such as the location the phone wrote into the photo, are cut out of the file without decoding or re-encoding it, so the pixels stay exactly as they were. The orientation of a JPEG or WebP photo is kept, so a photo does not show up sideways. A JPEG also keeps its JFIF header, its ICC color profile and its Adobe color segment, because they decide how the colors look and carry no details about the person or the place. A file whose structure does not parse is refused as an invalid image. Raw face photos are never accepted because mood detection runs on the device.
