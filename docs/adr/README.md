# Decision records

An architecture decision record says why a choice was made, so that nobody has to guess later and so that a change of mind starts from the reasons and not from the code. Each record has a status, the context, the decision and what follows from it. A record is not rewritten when the decision changes. A new record supersedes it, and the old one gets the status `Superseded by` and the number of the new one.

| Record | Status |
|:---|:---|
| [0001 PostgreSQL decides, Redis only helps](0001-postgresql-decides-redis-helps.md) | Accepted |
| [0002 Idempotency keys live in a middleware backed by PostgreSQL](0002-idempotency-middleware.md) | Accepted |
| [0003 Pull requests are merged with merge commits](0003-merge-commits-only.md) | Accepted |
| [0004 Only the release GitHub App creates version tags](0004-release-app-creates-tags.md) | Accepted |
| [0005 Chat sends over REST and receives over a WebSocket](0005-chat-rest-and-websocket.md) | Accepted |
| [0006 Errors go to Sentry through the core SDK, scrubbed and capped in our own code](0006-sentry-error-reports.md) | Proposed |

To add one, copy the layout of an existing record, give it the next number and a short name, and add it to the table. Keep it to what was decided and why, and link to the pages that describe the details.
