# Differences from the PRD

The PRD leaves a few gaps that this implementation fills.

- `class_teachers` (present in the ERD but not the DDL), `classes.join_code` and `guidance_applications` were added to the schema.
- `gen_random_uuid()` replaces `uuid_generate_v4()`, so no extension is needed.
- `POST /auth/register`, `POST /auth/refresh-token`, `POST /auth/logout` and `POST /users/avatar` were added, and `classOverview` also returns `joinCode`.
- The PRD defines points only for organic and inorganic trash, so B3 is recorded with `POINTS_B3` (0 by default).
- BR-04 (confirm AI results below 75% confidence) is a client dialog, so the server stores whatever confidence the confirmed result carries.
- `averageHappyScore` is the share of that day's students whose latest mood is SENANG.
- Resetting rankings at a new semester (BR-03) is not automated yet.
