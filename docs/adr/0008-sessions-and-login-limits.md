# 0008 Sessions end in PostgreSQL, and the API limits logins by account and not by address

Status: Accepted, 2026-10-21. This record was written after the fact. The details are in the [design notes](../design.md).

## Context

Sign in and sign up are open to anyone and each one checks or hashes a password with bcrypt, which uses a whole CPU for tens of milliseconds. The users are teachers and parents of one school, so many of them sit behind the same public address, on a school network or one mobile provider. The app queues actions while the phone is offline and retries them.

## Decision

A refresh token is random, is stored only as a SHA-256 hash, works exactly once and belongs to a token family that a single sign-in created. PostgreSQL alone decides whether a token is valid, with one atomic `UPDATE ... RETURNING`. Redis only remembers a used token for a minute, so a quick replay is refused without a database call ([0001](0001-postgresql-decides-redis-helps.md)). A used token that is shown again after that minute is taken for a theft, the whole family is deleted and everyone who holds a token of it has to sign in anew. A session ends `JWT_SESSION_MAX_DAYS` after the sign in however often it is refreshed.

No rate limit of the API looks at the address of the client. Only the limit in the reverse proxy does, see below. Every limit counts per user id, per account (the email, or the refresh token), or per route for the class code. A request that names no account is not counted, because it is rejected before the database. The counters live in Redis when it is reachable and in the memory of the process when it is not.

A device that signed in before gets a device token. It is signed with a key derived from the JWT secret, bound to the email and valid for 90 days, and it only picks which login bucket a request is counted in. A stranger who uses up the shared bucket of an email cannot keep the owner's devices from signing in. The token never replaces the password.

The number of passwords that are hashed at the same time is capped by `PASSWORD_HASH_CONCURRENCY`, one slot for each CPU except one, and never fewer than one. A request that finds every slot taken waits `PASSWORD_HASH_WAIT_MILLISECONDS` and is then refused with `429` and `Retry-After: 1`, so an app waits the way it does for any other limit.

A flood that names a new account in every request is not stopped by the API, because there is nothing to count. A limit in the reverse proxy for login and register is the answer, and the design notes hold the nginx example.

## Consequences

- Anyone can send `AUTH_LIMITER_MAX` failed logins for someone else's email and block new devices of that person for one window, and the devices that signed in before keep working.
- The class code is the only secret between a stranger and a class, so its budget is for each route and shared by every caller. A flood of guesses can keep parents from joining on that route for as long as it lasts, and the right code is refused too while the budget is gone.
- During a flood of sign ins some real ones are refused as well. Ordinary requests of people who are already signed in are not affected, but changing a password or deleting an account uses the same hash slots and can wait or get `429` while they are all taken.
- The proxy limit counts by address and is the one place where a school network can be throttled together, so it is only on login and register.
- Redis can be down or restored from an old copy without reviving a revoked session.
