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
- `POST /auth/login` accepts an optional `deviceToken`, and login and registration both return one. It gives a device its own login budget for an email.
- `GET /wali/dashboard` takes an optional `studentId` for a parent with several children and defaults to the child who was registered first. The `student` object also returns the `classId` of the child, which a parent needs for the `/classes/{classId}` routes.
- `GET /guru/dashboard` takes an optional `classId`, `classOverview` also returns `joinCode`, and `dominantMood` is `null` while the class has no mood record for the day.
- `GET /guru/mood/analytics` also returns `monthlyDistribution` when `range=monthly`.
- `GET` and `POST /classes/{classId}/videos` list and add learning videos. Both teachers of the class and parents of a child in it can list them, and only a teacher can add one. The video is an https address, either a link to another host or a file the teacher uploaded.
- `PATCH` and `DELETE /classes/{classId}/videos/{videoId}` change the details of a video and delete it. Any teacher of the class can do both. The address of a video cannot change, so a different file is a new video. Deleting an uploaded video also removes its file from the bucket once no other video uses it.
- `POST /classes/{classId}/videos/upload-url` signs an address for uploading an mp4 or webm file straight to the bucket, because a video is too large to pass through the API. The teacher sends the file there and then adds the video with the `videoUrl` of the answer. The file waits in a staging folder until it is added, and adding copies it to a permanent address that the video returns. Adding checks that the file arrived, belongs to the class and is a video of an accepted type and size.
- `GET` and `POST /classes/{classId}/forum` list and start threads, and `GET` and `POST /classes/{classId}/forum/{postId}/replies` list and add replies. Teachers of the class and parents of a child in it can all write. A reply cannot be answered again. The author of a deleted account shows as `Deleted account`.
- `PATCH` and `DELETE /classes/{classId}/forum/{postId}` and `/classes/{classId}/forum/{postId}/replies/{replyId}` change and delete a thread or a reply. Only the author can change a post. The author and the teachers of the class can delete one, and deleting a thread deletes its replies. A thread or a reply that is not where the path says returns `FORUM_POST_NOT_FOUND`. A thread and a reply carry `editedAt`, the time the author last changed the words, and it is `null` for a post that was never edited. A save that leaves the title and the text as they were does not count as an edit.
- The lists of videos, threads and replies take `page` and `limit` and return `page`, `limit` and `total` next to the items.
- `GET` and `POST /guru/classes` list and create classes, and `GET /guru/classes/{classId}` shows one with its number of children, learning videos and forum threads. A teacher can teach several classes and every class has its own join code, which the teacher replaces with `POST /guru/classes/{classId}/join-code`. The roadmap mentions six subjects on the class screen, which the PRD never defines, so the response carries counts instead.
- `GET /guru/classes/{classId}/teachers` lists the teachers of a class and `POST /guru/classes/{classId}/teachers` lets one of them add another registered teacher by email. The new teacher sees the whole class, so the route is only open to teachers of that class, and a teacher still cannot join a class on their own with its join code. One teacher owns each class, the one who made it at first. The owner takes any other teacher off the class with `DELETE /guru/classes/{classId}/teachers/{teacherId}` and hands the class to another teacher with `PUT /guru/classes/{classId}/owner`, and every teacher can leave with the same `DELETE` on their own id. The only teacher of a class cannot leave. When the owner leaves or deletes the account, the teacher who joined next becomes the owner.
- `GET /wali/children` lists the children of a parent and `POST /wali/children` adds one more with a class code, in the same class or another one, up to 10 per parent. A class code is guessable only through the shared budget, so this route uses `CLASS_CODE_LIMITER_MAX` like registration does. The `studentId` of the other parent routes picks the child.
- `GET /classes/{classId}/guidance-reports` shows the teachers of the class which guides the parents applied for their children, newest first, with the name of the child, the title and category of the guide and the notes of the parent. It is how a teacher knows that the handling is under way, and the PRD does not describe a route for it.
- `GET /classes/{classId}/students` lists the children of a class with their points, rank and latest mood of today. It is for the teachers of the class only because it shows the NISN.
- `GET /guru/profile` and `GET /guru/profile/detail` show the teacher, and `PUT /guru/profile` changes the phone number and the address. The address is a new `gurus.address` column and the photo is still changed with `POST /users/avatar`.
- `GET /wali/child/{studentId}` shows the registered details of a child together with the contact details of the parent, and `GET /trash/stats` shows the scans and points of a child for each trash category together with the scans left today.

## Not built yet

- Taking a teacher off a class. A teacher can be added but not removed, and a class without any active teacher stops taking in new children.
- Removing an uploaded video file that was never added to a class. Such a file waits under the `pending/` prefix of the bucket, and a bucket lifecycle rule that expires `pending/` after one day removes it. The rule is set in the bucket and not in this API, and it must never cover `videos/` because that holds the real videos.

## Language

Messages and the weekday names in `weeklyTrend` are in English (`Monday` to `Friday` where the PRD examples use `Senin` to `Jumat`), and new classes default to the grade level `Class A`. Clients branch on `errorCode` and translate the text they show.

## Business rules

- The PRD defines points only for organic and inorganic trash, so B3 is recorded with `POINTS_B3` (0 by default).
- BR-04 (confirm AI results below 75% confidence) is a client dialog, so the server stores whatever confidence the confirmed result carries.
- `averageHappyScore` is the share of that day's students whose latest mood is SENANG.
- BR-03 resets the rankings and not the points of a child. The leaderboard ranks `season_points`, which start from zero on the first day of each month in `SEMESTER_START_MONTHS` (January and July by default), while `current_points` keeps the points of all semesters. The API saves the final standings of the semester that ended in `leaderboard_history` first. No route reads that history yet.
