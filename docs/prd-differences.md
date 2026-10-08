# Differences from the PRD

The PRD leaves a few gaps that this implementation fills. The backend PRD in the app repository (`docs/prd_and_roadmap/02_BACKEND_API_AND_DATABASE.md`) describes the same contract as this implementation, so the list below is what changed compared with its first version.

## Schema

- `class_teachers` (present in the ERD but not the DDL), `classes.join_code`, `guidance_applications` and `refresh_tokens` were added to the schema.
- `users`, `gurus`, `walis` and `students` have a `deleted_at` column. Deleting an account hides the rows, and the unique rules on email, NIP and NISN only apply to rows that are still active, so those values can be used again.
- `gen_random_uuid()` replaces `uuid_generate_v4()`, so no extension is needed.
- `learning_videos` and `forum_posts` appear in the ERD without a DDL, so both tables were designed here. A video is an https address that a teacher adds, and a forum post is either a thread with a title or a reply that points at a thread of the same class.

## Endpoints

- `POST /auth/register`, `POST /auth/refresh-token`, `POST /auth/logout`, `POST /users/avatar` and `DELETE /users/me` were added, and `GET /healthz` reports the health of the service.
- A refresh token works once, and every successful refresh returns a new one.
- `POST /auth/login` and registration accept and return an optional `deviceToken`, which gives a device its own login budget for an email.
- `GET /wali/dashboard` takes an optional `studentId` for a parent with several children and defaults to the child who was registered first. The `student` object also returns the `classId` of the child, which a parent needs for the `/classes/{classId}` routes.
- `GET /guru/dashboard` takes an optional `classId`, `classOverview` also returns `joinCode`, and `dominantMood` is `null` while the class has no mood record for the day.
- `GET /guru/mood/analytics` also returns `monthlyDistribution` when `range=monthly`.
- `GET` and `POST /classes/{classId}/videos` list and add learning videos. Both teachers of the class and parents of a child in it can list them, and only a teacher can add one. The video is an https address and uploading a video file is not built yet.
- `GET` and `POST /classes/{classId}/forum` list and start threads, and `GET` and `POST /classes/{classId}/forum/{postId}/replies` list and add replies. Teachers of the class and parents of a child in it can all write. A reply cannot be answered again and a post cannot be edited or deleted yet. The author of a deleted account shows as `Deleted account`.
- The lists of videos, threads and replies take `page` and `limit` and return `page`, `limit` and `total` next to the items.
- The paths `/guru/classes`, `/guru/classes/{id}`, `/classes/{id}/students`, `/guru/profile`, `/guru/profile/detail`, `/wali/child/{id}` and `/trash/stats` are listed in the app roadmap but the PRD never defines them, so they are not implemented.

## Language

Messages and the weekday names in `weeklyTrend` are in English (`Monday` to `Friday` where the PRD examples use `Senin` to `Jumat`), and new classes default to the grade level `Class A`. Clients branch on `errorCode` and translate the text they show.

## Business rules

- The PRD defines points only for organic and inorganic trash, so B3 is recorded with `POINTS_B3` (0 by default).
- BR-04 (confirm AI results below 75% confidence) is a client dialog, so the server stores whatever confidence the confirmed result carries.
- `averageHappyScore` is the share of that day's students whose latest mood is SENANG.
- Resetting rankings at a new semester (BR-03) is not automated yet.
