# 0006 Errors go to Sentry through the core SDK, scrubbed and capped in our own code

Status: Proposed, 2026-10-20

## Context

Grafana shows that the API is unhealthy, and the JSON logs hold each failure, but nobody is told about a new kind of error, and the same error is not grouped across requests with its stack. Sentry does both. The project `kibooz-api` lives in the organization `syafa` in the EU data region.

The messages are about children and their parents, so what leaves the server has to be decided on purpose. A database error text can carry the values of a row, for example `Key (email)=(a@b.c) already exists`, so the safe default is to send little and to scrub what is sent. The plan is the free one after the trial, with 5000 errors a month. A limit for each client key is a feature of the Business plan, so it is not there. Spike protection of the organization is on, and it only works at the level of Sentry.

## Decision

The API uses the core package `github.com/getsentry/sentry-go` and nothing else from the SDK. The `fiberv3` package is left out on purpose, because its handler copies the request into every event, with the headers, the cookies and the query string.

Two places send an event. The error handler sends an error that becomes a `5xx` that the API did not choose, and the hook that logs a panic sends the panic, once, even though the recover middleware hands the same failure to the error handler afterwards. An answer such as `404` or `409` is part of the API and is never sent, and neither is a `503`, which says that something such as the object storage is not set up. Each event carries the release, which is the build version that `/healthz` also reports, the environment, the server name, the route pattern (not the address), the status, the request id and the id of the account. The request id is how the event is matched to the log line in Loki. Request bodies, headers, cookies, query strings, addresses of clients, breadcrumbs and the user name or email are never sent, and `SendDefaultPII` stays off.

A scrub runs in `BeforeSend` over every text of the event, the message, the exception values and the tags. It replaces emails, bearer tokens, JWTs and runs of five or more digits (a NISN or a phone number) with a placeholder. The scrub is a list of patterns, so it is best effort, and a test feeds it a database error with an email, a token and a NISN and checks that none of them leaves.

Our own cap sits in the same `BeforeSend`. At most `SENTRY_EVENTS_PER_MINUTE` events leave in one minute, 30 by default, and the rest are dropped and counted. A line is logged when a minute starts to drop events, and another with the count when that minute is over. A crash loop therefore cannot use the monthly quota in an hour, and the cap works on any plan. It does not replace spike protection, which stays on as the second layer.

The SDK is off when `SENTRY_DSN` is empty, and no client is created, so the capture calls do nothing and the tests and the development stack need no setting. `SENTRY_ENVIRONMENT` (default `production`) and `SENTRY_SERVER_NAME` (the host, empty by default) are the other two settings. On shutdown the process flushes the queue for up to two seconds, so the errors of a rolling deploy are not lost.

Errors only. Tracing, profiling, logs and metrics stay with Grafana, which already has them, and they would also use the quota and send more about the children.

## Consequences

- The module `sentry-go` is a new dependency, with a few indirect ones, and Renovate keeps it current.
- An event has no request, so a bug is reproduced from the request id and the log line, not from Sentry alone. That is the price of not sending request data.
- The Go error of a failed query has no stack of where it began, so the stack in an event is the one of the place that reports it. The text of the error and the route are what tell where to look.
- The scrub can miss a new kind of value. A review of the first events in Sentry and a test for each pattern that is added keep it honest.
- One project holds all the environments, and `SENTRY_ENVIRONMENT` tells them apart. When a staging stack exists it gets its own client key in the same project, so a leaked key is revoked without touching production. That key cannot have a rate limit of its own on this plan, so the cap of the code applies to it too.
- The DSN is not a password, but anyone who has it can send events into the project, so it stays in the `.env` of the server and out of the repository and the logs.
- `docs/privacy.md` says that error reports leave for Sentry in the EU region and what they hold.
