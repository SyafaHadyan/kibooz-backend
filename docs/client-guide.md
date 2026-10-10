# Client guide

This page is for the people who build an app against the API. The full contract is the [API reference](https://docs.kibooz.syafahadyan.com), and this page only explains the parts that an app has to do on its own.

## Base URL

The API is served over HTTPS at `https://kibooz-api.syafahadyan.com`. Every route lives under `/api/v1`, for example `https://kibooz-api.syafahadyan.com/api/v1/auth/login`, and only the health probe `GET /healthz` lives at the root. Plain HTTP requests are redirected to HTTPS, so always configure the app with the `https://` address.

## Sign in and keep the tokens

Registration and login return an access token and a refresh token. Use the access token on every request and keep both tokens in secure storage, which is the Keychain on iOS and the Keystore on Android.

A refresh token works once. Every successful call to `POST /api/v1/auth/refresh-token` returns a new refresh token, so replace the stored one each time and never reuse the old one. A call that fails returns an error and no token, and the error `AUTH_REFRESH_INVALID` means the session is over and the person has to sign in again. A session also ends by itself `JWT_SESSION_MAX_DAYS` days after the sign in, 90 by default, however often the token is refreshed. If an old refresh token is sent again a minute or more after it was used, the server takes it for a stolen token and ends the whole session, including the newest token, so the person has to sign in again. A client that does not save the new token before it sends the next request can cause this, so write the new token to storage first. Signing out ends the whole session. Send a refresh request when the access token expires and not on every launch.

## Keep the device token

Registration and login also return a `deviceToken`. It is not a credential and the app never needs it to sign in. It only gives this device its own login budget for that email, so someone who types wrong passwords for the same email cannot keep the owner from signing in.

- Store it next to the account, one token for each email, because a token only works for the email it was issued for.
- Send it as `deviceToken` in the next login request for that email.
- Replace the stored token with the one in every login response, because each login renews it for another 90 days by default.
- Keep it after logout. A token that was kept lets the device sign in again even while a stranger is using up the shared budget of the email.
- Do not worry about a stale one. A token that expired or does not match the email is ignored and the login is counted in the shared bucket. If that bucket is used up, the login returns `429` with `RATE_LIMITED`, the same as for a device that has no token.

A token refresh does not return a device token, so leave the stored one as it is.

## Class videos and forum

The learning videos and the forum of a class live under `/api/v1/classes/{classId}`. The teachers of the class and the parents of a child in it can read and write there, and anyone else gets `403` with `AUTH_FORBIDDEN`. A teacher finds the class id in `classOverview.classId` of the dashboard and a parent finds it in `student.classId` of the dashboard.

A teacher can create more classes with `POST /api/v1/guru/classes`, and every class has its own join code for parents. The register of a class at `GET /api/v1/classes/{classId}/students` is for teachers only. Only a teacher can add a video. A video is an https address that the player streams, either a link to another host or a file the teacher recorded. To upload a file, call `POST /api/v1/classes/{classId}/videos/upload-url` with the `contentType` (`video/mp4` or `video/webm`) and the exact `sizeBytes`, then send the file with a `PUT` to the `uploadUrl` of the answer with the `Content-Type` header it lists and nothing else, before `expiresAt`. Then add the video with `POST /api/v1/classes/{classId}/videos` and the `videoUrl` of the answer. Adding fails with `400` while the file has not arrived, so retry after the upload finishes. The upload address only holds the file for a while, so adding moves it to a permanent address and the added video returns that one as its `videoUrl`. Play the video from the address it returns and not from the one of the upload. A file that was just uploaded can be added once, and one that is never added is removed from the bucket after a day. The permanent address that the added video returns can be used again for another video of the same class, for as long as a video still uses the file. A file larger or of another type than announced is refused by the bucket. A teacher changes the title, description, thumbnail or duration of a video with `PATCH /api/v1/classes/{classId}/videos/{videoId}` and deletes it with `DELETE` on the same path. In a `PATCH`, a missing field stays, an empty `description` or `thumbnailUrl` clears it, and a `durationSeconds` of 0 clears the duration. An unknown video of that class returns `404` with `VIDEO_NOT_FOUND`. A thread has a title and a body, a reply has a body, and a reply cannot be answered again. The `author.id` of a post is the id of the account, so compare it with the id of the signed in user to mark your own posts. Posts of a deleted account stay and show `Deleted account` as the name. The author of a thread changes it with `PATCH /api/v1/classes/{classId}/forum/{postId}`, which takes a `title`, a `body` or both, and the author of a reply changes it with `PATCH` on `/forum/{postId}/replies/{replyId}`. Nobody else can change the words of a post, not even a teacher. The author can delete a post with `DELETE` on the same paths and so can a teacher of the class, and deleting a thread deletes its replies. Both are answered with `403` for anyone who may not, and with `404` and `FORUM_POST_NOT_FOUND` when the thread or reply is not at that path. Every thread and reply has an `editedAt` that is `null` until the author changes the words, so show an "edited" label when it is not `null`. A `PATCH` that leaves the words as they were does not set it.

Every list returns one page at a time. Send `page` (from 1) and `limit` (from 1 to 50, 20 by default) in the query string, and use `total` in the response to know when the last page is reached. A page past the end returns an empty list. Videos and threads come newest first and replies come oldest first.

## Handle rate limits

A request over a limit gets `429` with the code `RATE_LIMITED` and a `Retry-After` header. Wait that long before the next try and show the person a short message. `POST /api/v1/trash/scan-claim` can also answer `429` with `TRASH_DAILY_LIMIT_REACHED` once a child has used the claims of the day. That answer has no `Retry-After` header and the limit resets at midnight of the local day, so tell the person to come back tomorrow and do not retry. Always branch on `errorCode`. Do not retry in a loop, because every retry counts against the same limit. A `429` that has no JSON body comes from a proxy in front of the API and means the same thing.

The API never limits by network address, so a whole classroom on one network does not slow each other down. Routes for a signed-in user are limited per user. Registration and login are limited per email, or per device when the login carries a valid device token, and each of the two routes has its own budget. Refresh and logout are limited per refresh token, again with a budget for each route. A proxy or CDN in front of the API can add its own limit per address, and a very busy shared network can still get a `429` from that. The numbers and the reasons are in the [design notes](design.md#rate-limits).
