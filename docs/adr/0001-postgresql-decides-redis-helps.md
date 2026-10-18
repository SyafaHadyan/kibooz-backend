# 0001 PostgreSQL decides, Redis only helps

Status: Accepted

## Context

The API needs sessions that can be revoked, rate limits that several instances share, and a fast leaderboard. Redis is good at the last two, and it can be absent, restarted empty, or restored from an old copy. A school app that cannot sign anyone in because a cache is down would be a bad trade, and a restored cache that brings back a revoked token would be worse.

## Decision

PostgreSQL is the only authority for anything that must stay true. Refresh tokens are stored there as hashes and are used by one atomic update, and every sign in has a family so that a replay ends the whole session. Redis never decides that a token is valid. It remembers consumed tokens for a minute to refuse a quick replay without a database call, it holds the shared rate limit counters and it caches the leaderboard. The API starts and works without Redis. When Redis fails, the calls to it are skipped for a few seconds at a time, rate limits fall back to counters per process, the leaderboard is read from PostgreSQL and `/healthz` reports `degraded`.

## Consequences

- A Redis outage costs speed and some precision of the limits, and never costs access or correctness.
- A stale or restored Redis cannot revive a revoked token.
- Features that need a guarantee, such as the idempotency keys in [0002](0002-idempotency-middleware.md), go to PostgreSQL even when Redis would be faster.
- Redis can be a single server or a primary with replicas. Redis Cluster is not supported.

The details are in the [design notes](../design.md#redis-is-optional).
