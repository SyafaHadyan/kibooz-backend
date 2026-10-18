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
- Test coverage above the 80% that the product requirements ask for, with a CI pipeline that also runs load and security scans.

## Next

These are open and have no date.

- Push notifications. The product requirements ask for a notification when a teacher records a mood or posts a class task. The API has no way to register a device or send a message yet.
- Email. Verification before sign-in, notification mail and a way to reset a forgotten password all need an email service, which the API does not have. The design has to keep registration working in every test and scan, and nothing is decided yet.
- More documentation, namely an operations runbook, a release guide, a table of error codes and a description of the data model.

## Not planned

- A web interface. The API serves the mobile app and the interactive documentation.
- Social features that link children across classes, because of the privacy of the children.
