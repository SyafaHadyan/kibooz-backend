# Error codes

A failed response from the API has the same envelope, so an app can read the `errorCode` and ignore the rest of the text. A proxy in front of the API can answer with another body or none, so an app checks the HTTP status before it parses the body.

```json
{
  "success": false,
  "message": "Class code not found",
  "errorCode": "CLASS_CODE_NOT_FOUND"
}
```

The `message` is English text for people and can be reworded, so an app branches on `errorCode` and on the HTTP status and never on the message. A validation failure adds `details`, with one message for each field that is wrong, and a field inside a list is named like `entries[1].studentId`. The list is kept in `internal/apperror/apperror.go`, and the [OpenAPI reference](https://docs.kibooz.syafahadyan.com) shows which routes can answer each code. The end to end tests check every response against that spec, and the spec tells a client to treat a code it does not know like its status.

## Sign in and access

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `AUTH_INVALID_CREDENTIALS` | 401 | The email or password does not match the role that was chosen | Show a sign in error, the same text for a wrong email and a wrong password |
| `AUTH_TOKEN_MISSING` | 401 | The request has no `Authorization` header | Send the access token, or sign in |
| `AUTH_TOKEN_INVALID` | 401 | The access token is malformed, forged or expired | Refresh the token once and repeat the request |
| `AUTH_REFRESH_INVALID` | 401 | The refresh token was used before, expired, or its session ended | Send the user to sign in again, a session that ended cannot be revived |
| `AUTH_FORBIDDEN` | 403 | The signed in role may not use this route. On the `/classes/{classId}` routes (students, videos, forum, guidance reports) and on the leaderboard it is also the answer for a class that the account is not a member of, and for a class that does not exist. This holds for an id that is a valid UUID, a malformed one answers `400` with `VALIDATION_ERROR` | Do not show the action to that role, and treat it as a class that is gone on the class routes |
| `CHAT_CONTACT_NOT_ALLOWED` | 403 | A new conversation needs a teacher and a parent who share an active class, and a room needs an account that belongs to the class. A person who does not exist, was deleted, has the same role or shares no class, and a class that does not exist or that the account is not in, get this same answer | Show that this person cannot be contacted, and offer the contact list instead |
| `AUTH_PASSWORD_INCORRECT` | 403 | The current password sent to delete the account or change the password is wrong | Ask for the password again |

## Registration and conflicts

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `EMAIL_ALREADY_REGISTERED` | 409 | An active account has this email | Offer to sign in |
| `NIP_ALREADY_REGISTERED` | 409 | An active teacher has this NIP | Ask the teacher to check the number |
| `NISN_ALREADY_REGISTERED` | 409 | An active child has this NISN, at registration, when a parent adds a child or when a parent changes the NISN of a child | Ask the parent to check the number |
| `CHILD_LIMIT_REACHED` | 409 | The parent already has 10 children | Tell the parent that no more children can be added |
| `CLASS_JOINING_CLOSED` | 409 | The teachers of the class closed it for new children, so the join code does not work at registration, when a parent adds a child or when a parent asks for a move | Tell the parent to ask the teacher to open the class again |
| `CLASS_FULL` | 409 | The class already has 60 active children, so it cannot take another one at registration, when a parent adds a child, when a parent asks for a move or when a teacher accepts one | Tell the parent to contact the teacher |
| `ALREADY_IN_CLASS` | 409 | The child is already in the class whose code was sent in a move request | Tell the parent that the child is in that class |
| `MOVE_REQUEST_PENDING` | 409 | The child already has a move request that waits for a teacher | Show the request that waits, and offer to withdraw it |
| `CLASS_LIMIT_REACHED` | 409 | The teacher already teaches 20 classes that are not archived. The same answer comes when the teacher who is being added to a class is at 20 | Tell the teacher that no more classes can be created or joined |
| `MOOD_LOG_LIMIT_REACHED` | 409 | The child already has 20 mood logs on that local day. On a batch, `details` names the entries of the children who have no room left and nothing is saved | Tell the teacher which children to skip, and do not retry the same entries until the next day |

Email, NIP and NISN can be used again after the account that held them is deleted.

## Classes and teachers

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `CLASS_CODE_NOT_FOUND` | 404 | No open class has this join code. A class that was archived answers the same way | Ask the parent to check the code with the teacher |
| `CLASS_NOT_FOUND` | 404 | The class does not exist, or the signed in teacher does not teach it. Both get the same answer on purpose. It is the answer of the `/guru/classes/{classId}` routes, and of `/leaderboard` when it names no class and the caller has none. The `/classes/{classId}` routes answer `403` with `AUTH_FORBIDDEN` instead | Return to the class list |
| `CLASS_NO_ACTIVE_TEACHER` | 409 | The class has no active teacher, so it cannot take new children | Tell the parent to contact the school |
| `TEACHER_NOT_FOUND` | 404 | No active teacher account has the email that was named, as a new teacher or as the successor | Ask the teacher to check the email |
| `CHAT_SENDING_CLOSED` | 409 | The two people of a direct conversation no longer share an active class, the other account was deleted, or the class of a room is archived. Reading stays open | Keep the history, and replace the text box with a note that the conversation is closed |
| `TEACHER_ALREADY_IN_CLASS` | 409 | The teacher already teaches the class | Nothing to do, the teacher is already there |
| `TEACHER_NOT_IN_CLASS` | 404 | The teacher named in the path does not teach this class | Refresh the teacher list |
| `CLASS_OWNER_REQUIRED` | 403 | Only the owner of the class may remove another teacher or hand the class over | Hide the action from teachers who are not the owner |
| `LAST_TEACHER` | 409 | The only teacher of a class cannot leave it | Hand the class over or add another teacher first |
| `SUCCESSOR_REQUIRED` | 409 | The account is the only teacher of a class that has children and no `successorEmail` was sent when deleting it | Ask for the email of another teacher and send it again |

## Data that was not found

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `NO_CHILD_REGISTERED` | 404 | The parent has no child, because every child was deleted, so there is no default child for the dashboard or the trash statistics | Show the screen that adds a child with a class code |
| `STUDENT_NOT_FOUND` | 404 | The child does not exist, or the signed in user may not see it | Refresh the list of children |
| `MOVE_REQUEST_NOT_FOUND` | 404 | The move request does not exist, was withdrawn, was already decided or rejected by a class that closed joining, or belongs to another class or parent | Refresh the list of requests |
| `PROFILE_NOT_FOUND` | 404 | The profile of the account does not exist, for example after it was deleted | Sign the user out |
| `GUIDANCE_NOT_FOUND` | 404 | The guidance does not exist | Refresh the guidance list |
| `FORUM_POST_NOT_FOUND` | 404 | The thread or reply does not exist, or was deleted | Refresh the thread or its replies |
| `VIDEO_NOT_FOUND` | 404 | The learning video does not exist, or was removed | Refresh the video list |
| `CHAT_CONVERSATION_NOT_FOUND` | 404 | The conversation does not exist, or the signed in account is not one of its two people or a member of the class of the room. Both get the same answer on purpose | Refresh the conversation list |
| `CHAT_MESSAGE_NOT_FOUND` | 404 | The conversation has no message with this id | Refresh the messages |
| `NOT_FOUND` | 404 | The route does not exist | This is a bug in the app |

## Limits

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `RATE_LIMITED` | 429 | A rate limit was passed, or the server was too busy hashing passwords to take a sign in, sign up, password change or account deletion. The `Retry-After` header holds the seconds to wait | Wait that long, and send nothing in between because refused requests are counted too |
| `TRASH_DAILY_LIMIT_REACHED` | 429 | The child used all the point claims of today. There is no `Retry-After` | Show that the limit starts again on the next local day |

A proxy in front of the API can also answer `429` with no body and no `Retry-After`, so an app checks the status code first. The [client guide](client-guide.md) describes the wait and the back off.

## Retrying safely

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `IDEMPOTENCY_IN_PROGRESS` | 409 | A request with the same `Idempotency-Key` is still running | Wait a moment and send the same request with the same key |
| `IDEMPOTENCY_KEY_REUSED` | 422 | The `Idempotency-Key` was used for a different request | Use a new key for the new action. This is a bug in the app when it happens for a retry |

## Files

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `INVALID_IMAGE` | 400 | The file is not a JPEG, PNG or WebP image | Ask for another photo |
| `IMAGE_DIMENSIONS_TOO_LARGE` | 400 | The image is more than 8192 pixels on a side or 40 megapixels | Shrink the photo before sending it |
| `FILE_TOO_LARGE` | 413 | The file is larger than the limit | Shrink the photo, the app should do this before the upload anyway |
| `BODY_TOO_LARGE` | 413 | A JSON body is larger than 64 KB. Only the photo uploads (`/users/avatar`, `/trash/scan-claim` and `/chat/conversations/{conversationId}/media/photo`) are allowed more | A real request never gets near this, treat it as a bug in the app |
| `STORAGE_UNAVAILABLE` | 503 | Object storage is not configured, so uploads are off | Hide uploads, retrying will not help |
| `STORAGE_ERROR` | 502 | The bucket refused or failed a storage operation | Try again later |

## Everything else

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `VALIDATION_ERROR` | 400 | A field is missing or wrong, see `details`. A missing body or broken JSON is reported under the field `body`, and text with a NUL character is refused too | Show each message next to its field |
| `METHOD_NOT_ALLOWED` | 405 | The route does not answer this HTTP method | This is a bug in the app |
| `BAD_REQUEST` | 4xx | The HTTP layer refused a request that no other code describes. A request with a non-empty `Content-Encoding` other than `identity`, such as `gzip` or `zstd`, gets `415` with this code | This is a bug in the app. Send JSON bodies as they are, with no compression |
| `UPGRADE_REQUIRED` | 426 | A plain request reached the chat socket route, `GET /chat/ws`, which only takes a WebSocket handshake | Open it with a WebSocket client, see [Chat socket](websocket.md) |
| `CHAT_SOCKETS_FULL` | 503 | The copy of the API that got the request holds as many chat sockets as it may, or is shutting down | Connect again in a few seconds and keep polling until it works |
| `INTERNAL_ERROR` | 500 | Something unexpected failed. The details are logged with a request id and never sent | Try again later, and report the `X-Request-Id` response header if it keeps happening |
