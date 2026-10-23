# 0011 The service runs as two small copies behind nginx

Status: Accepted, 2026-10-21. This record was written after the fact. [Operations](../operations.md) has the settings and the steps.

## Context

Production is one VPS with 2 vCPU and 1.9 GiB of RAM, with swap. A single API process that hashes passwords can use a whole CPU for a moment, and a deploy restarts it for a few seconds. Nobody wants a deploy to be a visible outage, and nobody wants a runaway request to take the database down with it. The budget allows no second server.

## Decision

The API runs as two copies of the same image, each on its own port on the host, and nginx (the swag container) balances between them. A copy is capped at half a CPU and at the memory of `API_MEMORY_LIMIT`, which is 384 MiB in the bundled `compose.yaml`, and the two copies of a host have a Compose file of their own with `api-1` and `api-2` that is not in this repository. `GOMEMLIMIT` is set below the cap, so the Go runtime frees memory before the container is killed, and a memory problem kills one API copy and never the database. `PASSWORD_HASH_CONCURRENCY` is 1 for each copy, because a half CPU cannot hash two passwords at once. It limits how many hashes run together and does not reserve CPU for other requests, so one hash can still use the whole share of its copy ([0008](0008-sessions-and-login-limits.md)).

Nothing that has to be correct lives only in the memory of one copy. Sessions, idempotency keys and the order of messages are in PostgreSQL ([0001](0001-postgresql-decides-redis-helps.md), [0002](0002-idempotency-middleware.md)), and Redis holds only what can be lost. The one exception is on purpose. A rate limit counter that falls back to memory when Redis is down is kept for each copy, so the limit is then twice as high, and the docs say so.

Each copy keeps a pool of 25 database connections and one more for the health check, so two copies use 52 of the 200 that PostgreSQL allows, and the database has room for migrations and for a person with a shell. PostgreSQL runs in a separate stack with no memory cap. Redis is capped at 192 MiB and evicts only keys that have an expiry.

Migrations run at start-up, so the first copy to start applies them. For that to be safe, a migration file is never edited after it is merged and every new migration is a new file, and a migration must work with the previous version of the code still running, because a deploy restarts the copies one after the other.

A second copy is added by giving a service the labels of the first and a port of its own, and the monitoring finds it without a change ([0009](0009-monitoring-with-grafana-cloud-and-alloy.md)).

## Consequences

- A deploy can restart the copies one at a time, and nginx sends traffic to the one that is up.
- Two copies share one CPU budget of a single VPS, so a big burst of logins is limited by the hash slots and not by the CPU, and the proxy limit stays the first defense against a flood.
- Anything that needs to be in one place, such as a WebSocket fan-out, a job that must run once or a counter, needs PostgreSQL, Redis or a lock and cannot rely on memory. The chat design follows this ([0005](0005-chat-rest-and-websocket.md)).
- The compose file in the repository starts one copy. `compose.production.example.yaml` shows the two copies with their caps and is checked by `docker compose config` in CI, and the nginx configuration is part of the deployment on the host and not of the repository.
- A move to a bigger machine changes the numbers, and it should change them in the settings and this record together.
