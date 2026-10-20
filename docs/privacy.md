# Privacy

This page describes, from the code, which personal data Kibooz Backend keeps, who can read it, how long it stays and what deleting an account really does. It is a technical description for the people who run the service and for the app team, and it is not a legal notice. The children are young, so it is written to be checked against the code and not to sound reassuring. The protections around the data are in [Security](security.md), and the tables are in the [data model](data-model.md).

## What is kept

| Who | Data | Where |
|:---|:---|:---|
| Every account | Email, a bcrypt hash of the password, the role, the full name, a phone number if given, an avatar photo if uploaded | `users` and the bucket |
| A teacher | NIP, school name, address | `gurus` |
| A parent | Address, WhatsApp number | `walis` |
| A child | Full name, NISN, class, avatar photo if uploaded, points and rank | `students` and the bucket |
| A child, recorded by a teacher | Each mood with its confidence score, the source, the teacher's notes and the time it was seen | `mood_logs` |
| A child, claimed by a parent | Each trash sorting claim with the category, the points and the photo of the trash | `trash_scans` and the bucket |
| A child, by a parent | The guidance that the parent applied and the parent's notes | `guidance_applications` |
| A class | Its name, grade, school, join code and the teachers | `classes` and `class_teachers` |
| A teacher or a parent | Forum threads and replies, and the videos that a teacher added | `forum_posts` and `learning_videos` |
| A teacher or a parent, in the chat | Each message with its text, an optional photo or video, its sender and time, and how far each person has read | `chat_conversations`, `chat_messages` and `chat_reads`, and the files in the private bucket |
| A session | A SHA-256 hash of each refresh token, never the token | `refresh_tokens` |

The PRD requires that the raw photo of a face stays on the device, and the API has no route that accepts one. A teacher sends the mood that the app detected with its confidence score. The photos that do reach the server are the avatars and the photos of sorted trash, and they are checked to be real images of a limited size.

Passwords are never stored in clear text or logged. The server holds the hash only. A request body is not written to the access log, and the database logger keeps the values of a query out of its lines.

## Who can read it

- A **parent** reads their own account, their own children, the mood, points and guidance of those children, the leaderboard of the class of a child, and the forum and the videos of that class. The leaderboard shows the name, the rank, the points and the avatar of every child in the class, so a parent sees the names of the other children and their points.
- A **teacher** reads the classes they teach, with the full roster of children including each NISN and the mood of today, the mood analytics of the class, the guidance reports with the notes that parents wrote, and the class forum. A teacher also sees the name, NIP and photo of the other teachers of the class. A teacher cannot read a class they do not teach, and the API answers the same way as for a class that does not exist.
- A **chat conversation** is read only by its two people, a teacher and a parent, and everyone else gets `404`. A teacher is offered the parents who have an active child in one of their classes, with the names of those children, and a parent is offered the teachers of the classes of their children. Nothing is stored for that list, it is worked out from the classes. Parents never see each other in a private conversation, and a conversation is never shown to a teacher who is not one of the two. The photos and videos of the chat are never public. They sit in a private bucket and the API hands only a member of the conversation a signed address that stops working after `CHAT_MEDIA_URL_SECONDS`. The metadata of a photo, such as the location, is removed on upload, and a video is sent by the app straight to the bucket, so the app should not record a location into it. The **room of a class** is read by the teachers of the class and the parents with an active child in it, who see each other's names and photos there, and each person sees only the messages sent after they joined the class. A parent keeps the room while any child of theirs is still in the class and loses it when the last one leaves, and the teachers keep it.
- A **forum post** shows the full name, the role and the avatar of its author to everyone in the class. A deleted account shows as `Deleted account` with no photo.
- The email, phone number, WhatsApp number and address of a parent are returned only to that parent, in the profile of their child. No route returns a password hash.

## How long it stays

| Data | Kept for |
|:---|:---|
| Account, child, mood, scan, guidance, forum, chat and video rows | Until the project owner removes them from the database. Nothing in the code removes them on a timer |
| Refresh tokens | Until they expire or the session ends. A used token stays for a week so that a replay can be recognized |
| Remembered `Idempotency-Key` rows, which hold the response of the request | `IDEMPOTENCY_TTL_HOURS`, 7 days by default, and an hourly job removes the older ones |
| A deleted chat message | The row stays with an empty body, so the text is gone and a placeholder is left in the conversation. Its photo or video is queued for removal from the private bucket in the same step and is deleted within minutes |
| Chat photos and videos | In the private bucket until the message is deleted by its author. A file that was uploaded and never sent is removed after one day by the lifecycle rule on `pending/`. A message stays after its sender deletes the account, and so does its file, because the other people of the conversation still need the history |
| Semester standings in `leaderboard_history` | Without an end. They keep the name of the child as it was when the semester closed |
| Photos in the bucket | Until the account, or the child that the photo belongs to, is deleted. A video file that was uploaded and never added is removed after one day when the bucket has the lifecycle rule on `pending/` that the [README](../README.md#configuration) asks for |
| Error reports at Sentry | For the retention of the Sentry plan, which the owner can shorten in the project settings. Nothing in the code deletes them, and deleting an account does not remove them. They hold the id of the account and no name or email |
| Access log lines | As long as the container logs are kept, at most five files of 10 MB with the bundled Compose file |

The access log holds the time, the client address, the method, the path, the status, the time taken and a request id. With `TRUST_PROXY` off, the address is the one of the reverse proxy.

## What deleting an account does

For a teacher or a parent account, `DELETE /users/me` asks for the password and then does the following in one transaction.

- Hides the account, and for a parent every child, by setting `deleted_at`. The API no longer returns them and the email, NIP and NISN can be used for a new account.
- Ends every session by deleting the refresh tokens.
- Clears the addresses of the avatar of the account, of the avatars of its children and of the photos of their trash claims, and queues the files for removal from the bucket. A background job removes them and retries the ones that fail.
- For a teacher, hands the classes on to another teacher, or archives a class that has no children.

What stays is the other part of the data. The rows are hidden and not erased, so the name, the email, the NISN, the moods with their notes, the claims and the guidance remain in the database, and so do the forum posts, which show as `Deleted account`. The name of a child also stays in the closed semester standings. Erasing a person completely needs the owner of the database to remove those rows by hand, and the API has no route for it.

## Other places the data goes

| Where | What | Why |
|:---|:---|:---|
| The S3 compatible bucket and its public address | Avatars, trash photos and class videos | The app shows them by address, so the objects are readable by anyone who has the address. The addresses are long random keys, but they are not secret |
| Cloudflare, as this deployment is set up | The photos that pass through the media host, the documentation, and the logs that Cloudflare keeps | It serves the media and resizes the avatars with its image service |
| Sentry, in the EU region, when `SENTRY_DSN` is set | An error report for a failure of the server (a 5xx or a panic) with the error text, the stack, the route pattern, the status, the request id, the release, the environment, the host and the id of the account. Emails, tokens and long numbers are replaced in every text, and a request body, a header, a cookie, a query string, an address of a client and a log line are never sent | To tell the owner about a new kind of error, grouped with its stack. The data about children stays out of it by design, see [ADR 0006](adr/0006-sentry-error-reports.md) |
| Redis | The shared rate limit counters, which hold only hashed identifiers, the one minute memory of used refresh tokens and the leaderboard cache | A cache, never the source of truth |

The code makes no call to an analytics, advertising or tracking service, and it sends no email. Its only outgoing connections are to PostgreSQL, Redis, the bucket and, when `SENTRY_DSN` is set, Sentry.

## Keeping the data small

- Take a dump of the database only when needed, and keep it as protected as the database.
- Do not paste responses, logs or dumps into an issue. The bug report form asks for secrets and personal data to be removed first.
- Use throwaway accounts for tests. The smoke test registers accounts with random names and removes them again, and the CI scans run against a throwaway stack that is discarded.
