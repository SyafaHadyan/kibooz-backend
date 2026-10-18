# 0002 Idempotency keys live in a middleware backed by PostgreSQL

Status: Accepted, 2026-10-18

## Context

The app queues actions while the phone is offline and repeats them until one works. Without protection, a repeated mood log is stored twice and a repeated trash claim spends the daily limit of five. A response can be lost on the way back even when the work succeeded, so the app cannot know whether to repeat.

## Decision

The routes that create something accept an `Idempotency-Key` header. A middleware claims the key with one `INSERT ... ON CONFLICT` before the work, stores a fingerprint of the request, and stores the response after a `2xx`. A repeat of a finished request gets the stored response again with `Idempotent-Replayed: true`, the same key with another request is refused, and a repeat while the first try runs is told to wait. The keys live in the PostgreSQL table `idempotency_keys` and are kept for `IDEMPOTENCY_TTL_HOURS`, 168 by default. Each claim has its own id, so a try that was taken for dead cannot change the row of the try that replaced it.

Two other designs were considered. Keeping the keys in Redis would turn every repeat into a duplicate during an outage, which breaks [0001](0001-postgresql-decides-redis-helps.md). Writing the claim inside the transaction of each route would close the gap below, and it needs a change in every repository.

## Consequences

- One piece of code protects every creating route, and adding the header to a new route is one line.
- The work and the stored response are written in two steps. If the process dies between them, a try after the abandon time repeats the work. This is rare, and it is the known cost of the middleware approach.
- A response over 1 MiB is not stored. No route produces one today.
- Only an offline queue with keys benefits, so the app has to send them. The [client guide](../client-guide.md) and the [design notes](../design.md#idempotency-keys) say how.
