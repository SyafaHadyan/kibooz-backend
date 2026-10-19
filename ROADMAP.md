# Roadmap

This is the roadmap of the backend. The product plan, with its three phases, is the set of documents in the [app repository](https://github.com/ValentinusMarvel/kibooz-app/tree/main/docs/prd_and_roadmap). This page shows what the API offers for each phase and what is still open. When one of the two changes, the other is updated in the same sitting. The release history is in [CHANGELOG.md](CHANGELOG.md), and the list of places where the API departs from the product requirements is in [docs/prd-differences.md](docs/prd-differences.md).

A roadmap is a plan and not a promise. Items move, and the maintainer decides the order.

## Phase 1, the core loop

The core loop is done. It covers registration and login with refresh tokens, teachers and parents with their classes and children, mood logs that show on the parent dashboard, trash scan claims that add points, and the class leaderboard.

## Phase 2, AI vision and gamification

The API side is done. The machine learning models run on the device, so the backend only receives their results.

- Mood logs for one child or for a whole class at once, which suits a camera scan that sees many faces.
- Trash scan claims with an optional photo as evidence, a daily limit and points by trash type.
- Learning videos as https links, including files that a teacher uploads straight to the bucket with a signed address.
- Parent guidance, a class forum and teacher tools such as class teams, owners, join code replacement and archiving.

## Phase 3, hardening for real schools

Most of the backend work for this phase is done.

- Offline support. A request can carry an `Idempotency-Key`, so an app that replays a queued request does not create it twice. A queued mood log can carry the time it was seen in `recordedAt`.
- Accounts. A person can change the password, and can delete the account with the children and files that belong to it.
- Reliability and safety. Rate limits, a health check that reports a degraded state, signed releases, and security and quality checks on every pull request.
- Documentation. An operations runbook, a release guide, a table of error codes, a description of the data model, a privacy page, decision records and a glossary.
- Test coverage above the 80% that the product requirements ask for, with a CI pipeline that also runs load and security scans.

## Next

These are open and have no date.

- Monitoring. The API writes a JSON access log with a request id and has a health check, and Better Stack watches that endpoint and the documentation site and alerts the owner, with a public status page. The API also serves metrics on a private listener, and the repository holds the Alloy setup, the dashboards and the alert rules for Grafana Cloud, see [Monitoring](docs/monitoring.md). They only run once the Grafana Cloud values are set on the host. Error reporting for server errors is a separate step that is not built yet.
- Push notifications. The product requirements ask for a notification when a teacher records a mood or posts a class task. The API has no way to register a device or send a message yet.
- Email. Verification before sign-in, notification mail and a way to reset a forgotten password all need an email service, which the API does not have. The design has to keep registration working in every test and scan, and nothing is decided yet.
- Google sign in. A person could sign in with a Google account instead of a password. The API cannot create an account from a Google sign in alone, because registration needs a role and a class code or a NIP. A Google only account also needs another way to confirm its identity when it changes the password or deletes the account, and a rule for an email that already has a password account. Nothing is decided yet, and it relates to the email item above, because Google returns a verified email.
