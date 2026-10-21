# Data model

PostgreSQL is the only authority for the data of Kibooz. The schema is built by the SQL files in `internal/infra/db/migrations`, which the server applies when it starts, see [Operations](operations.md#migrations). This page explains what each table holds and which rules the database enforces itself, so a mistake in the code cannot break them. The original design is in the PRD of the Android app, and where the schema differs from it the [PRD differences](prd-differences.md) page says so.

## Tables

| Table | What a row is |
|:---|:---|
| `users` | One account with an email, a password hash, a role (`GURU`, `WALI` or `ADMIN`), a name, a phone number and an avatar address |
| `gurus` | The teacher part of a `GURU` account, with a NIP, a school name and an address |
| `walis` | The parent part of a `WALI` account, with an address and a WhatsApp number |
| `classes` | A class with a unique join code, a name, a grade level and an academic year. `archived_at` is set when nobody teaches it and it has no active child. `join_open` is false while its teachers do not want new children |
| `class_teachers` | A teacher of a class. A class can have several teachers, and `is_owner` marks the one who owns it |
| `students` | A child, with a parent, a class, a NISN, the points of all semesters (`current_points`), the points of the running semester (`season_points`) and the rank |
| `enrollments` | A child in a class, asked for, current or over, with a status of `pending`, `active`, `ended` or `rejected`. Every child has one active enrollment, and the points, rank and scans it had are saved on it when it ends. See [ADR 0014](adr/0014-class-enrollments.md) |
| `mood_logs` | One mood that a teacher recorded for a child, with the mood, a confidence score, the source, notes and the time it was seen |
| `trash_scans` | One verified trash sorting claim, with the category, the points awarded and the photo |
| `guidance_applications` | A parent who applied a piece of emotional guidance to a child, with their notes |
| `learning_videos` | A video that a teacher added to a class |
| `forum_posts` | A thread or a reply in the forum of a class |
| `chat_conversations` | A conversation, either `direct` or `class`. A direct one is between a teacher and a parent, one for each pair of accounts, and the smaller id of the two is always `user_low_id`. A class room has a `class_id`, one room for each class, and no pair. Its members are not stored, they are the teachers of the class and the parents with an active child in it, and each sees the messages from the time they joined |
| `chat_messages` | A message of a conversation. The `id` only grows and is the cursor of the conversation. A message has text, a photo or video, or both, and the four `media_` columns are set or empty together. A deleted message keeps its row with an empty body, no media and `deleted_at` |
| `push_tokens` | The token that Firebase gave an installed app, one row for each device and each token on one account at a time. The tokens of an account are removed when the account is deleted |
| `chat_reads` | How far each person of a conversation has read, the id of the last message |
| `refresh_tokens` | A refresh token of a session, stored only as a SHA-256 hash |
| `leaderboard_seasons` | One row for every semester that has begun |
| `leaderboard_history` | The final standings of a class for a semester that ended |
| `file_cleanups` | A bucket file that waits to be removed again after a failed try |
| `idempotency_keys` | A remembered `Idempotency-Key` with the response of the request |

The enum types are `user_role`, `mood_enum` (`SENANG`, `SEDIH`, `MARAH`, `BINGUNG`), `input_source_enum` (`AI_CAMERA`, `MANUAL_INPUT`) and `trash_category_enum` (`ORGANIK`, `ANORGANIK`, `B3`).

## Who belongs to whom

Registration creates one matching `gurus` row for a `GURU` user and one matching `walis` row for a `WALI` user in the same transaction. The schema only allows at most one role row per user, through a unique foreign key. It does not require the row and does not check that it matches `users.role`. A parent owns children, and each child belongs to one class. A class is taught by any number of teachers through `class_teachers`. Everything a child produces, which means moods, scans and guidance, points at `students`. The forum and the videos point at the class and at the teacher or user who wrote them.

## Rules the database enforces

- **Active uniqueness.** The email of a user, the NIP of a teacher and the NISN of a child are unique only among rows where `deleted_at` is empty. A deleted account keeps its row, and its email, NIP and NISN can be used again. The index names are the same as the old constraints, and the API turns a clash into `EMAIL_ALREADY_REGISTERED`, `NIP_ALREADY_REGISTERED` or `NISN_ALREADY_REGISTERED`.
- **Join codes.** `classes.join_code` is unique across all classes, archived ones included, so a code is never handed to another class. The format is an application rule and not a database constraint, so the code that makes a code uses eight characters from an alphabet without look alike characters. The migration that made codes longer replaced every shorter existing code.
- **One owner per class.** A partial unique index on `class_teachers` allows at most one row with `is_owner` for a class, so two changes that overlap cannot leave a class with two owners.
- **Enrollments.** A partial unique index allows at most one `active` and at most one `pending` enrollment for a child, and checks tie the dates and the saved numbers to the status. A trigger on `students` opens the active enrollment of every new child in its class and ends it with the saved points, rank and scans when the child is deleted, so no code path can forget it. The class in `students` still names the class of the active enrollment, and the routes that move a child will keep the two in step.
- **Points never go negative.** `current_points`, `season_points` and `points_awarded` have `CHECK` constraints, and so does a confidence score, which has to be between 0 and 1.
- **Forum shape.** A thread has a title and no parent, and a reply has a parent and no title. A check makes the title and the parent agree. A composite foreign key on the parent and the class keeps a reply in the same class as its parent post. The application separately requires the parent of a reply to be a thread.
- **Refresh tokens.** The hash is unique. A used token keeps its row with `used_at` set, so that a second use is seen as a replay, and every token has a `family_id` that its session shares.
- **Idempotency.** `(user_id, idempotency_key)` is the primary key, so two requests with the same key cannot both claim it. Taking over a stale claim is guarded by the application and not by the database. Each claim has its own `claim_id`, and every update or delete of a claim names it in its `WHERE` clause, so a try that was taken for dead matches no row and cannot change the row of the try that took over.

## Deleting

Accounts and children are never removed. `DELETE /users/me` sets `deleted_at` on the user, on the role row and, for a parent, on every child, and the API treats a row with `deleted_at` as gone. Rows that point at a child, such as the moods and scans, stay with the hidden child, and the forum posts and videos of a deleted teacher stay in their class. The foreign keys that cascade only matter when a row is removed for real, which the application does not do for people.

Three things are removed for real. The refresh tokens of the account, so every session ends. The addresses of the avatar of the account, of the avatars of its children and of the photos of their trash scans, which are cleared from the rows and queued in `file_cleanups` for a background job to remove from the bucket. And for a parent, the ranking of every class their children were in is recomputed without them.

A teacher account first hands its classes on. A class that another teacher also teaches stays with them, and the next teacher becomes the owner when the deleted one owned it. For a class that nobody else teaches, the result depends on the `successorEmail` of the request. When it names another active teacher, that teacher takes every such class, empty ones included. When it is sent but names nobody who can take over, the deletion is refused. When it is left out, a class with an active child makes the deletion fail with `SUCCESSOR_REQUIRED`, and a class with no active child is archived, which keeps its row and its join code.

## Concurrency

Some rules cannot be written as a constraint. The number of teachers of a class, registering a child with a join code and the archive of an empty class all depend on a count that two requests could read at the same time. They take a PostgreSQL advisory lock named after the class (`teachers:<class id>`) for the length of the transaction, in `internal/classteam`, so only one of them reads and changes the class at a time. The ranking of a class and the files of a video address have their own advisory locks in the same way. The leaderboard reset uses the `leaderboard_seasons` row the same way, so it runs once for a semester whichever instance of the API sees the date first. A password change and an account deletion take the row of the user first, then the refresh tokens, then the rest. A sign in reads that row under a share lock before it stores its session, so a sign in that is under way finishes before the sessions are ended and one that starts later finds the new password or the deleted account. Anything that takes these locks in another order can deadlock with them, and `internal/sessions` explains why.

## Changing the schema

A change is a new pair of files, `NNNNNN_what_it_does.up.sql` and `.down.sql`, with the next number. Never edit a file that was merged, because other databases have already applied it. A test fails when a number is missing, used twice, or has no down file. Write the reason for the change at the top of the file as a comment, and add the table or the column to this page and to the PRD of the Android app in the same change.
