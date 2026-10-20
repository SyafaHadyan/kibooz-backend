# 0013 A closed app is woken by a push without text, and fetches the message from the API

Status: Proposed, 2026-10-21

## Context

The chat socket ([0005](0005-chat-rest-and-websocket.md)) only reaches an app that is open. A parent whose app is closed should still learn that the teacher wrote, as a notification on the phone. On Android the way to reach a closed app is Firebase Cloud Messaging (FCM), which means the message goes through servers of Google.

The messages are about children, so the choice is what Google gets to see. The app shows a notification with the name of the sender and the first line of the message, as every chat app does, but that does not mean the text has to travel through Google to get there.

## Decision

The backend sends a data-only message through FCM with high priority and nothing that a person can read. It carries the `conversationId` and the `messageId` and no text, no name of the sender and no `notification` block. A collapse key per conversation makes a burst of messages wake the app once.

The app wakes, refreshes its access token when it has to, asks the API for what it missed with `GET /chat/conversations/{conversationId}/messages?after=` and builds the notification itself on the phone, with the name of the sender and the first line. When that fails it shows a plain "New message". The lock screen shows only that there is a new message, and the app has a setting for the previews that is on by default. This is how Signal does it. The text never passes through Google, and the push reuses the authentication, the limits and the cursor that the app already has.

The backend keeps a table of push tokens, one row for each device of an account, with routes to register a token and to remove it, which the app calls after sign in, when Firebase hands out a new token and at sign out. A token that FCM reports as unregistered is deleted. An account that is deleted loses its tokens with its sessions.

A push is sent after the message is stored, to the people of the conversation who are not the sender. A class room pushes to its members like a private conversation does, and the collapse key keeps the phone quiet. The send never fails the request that stored the message, it runs after the response, and a failure is logged and counted. A person whose app is open also gets the push, because the server cannot tell cheaply across copies who has a socket open. The app that is in the foreground ignores the wake-up, since the socket already told it, and one that is in the background fetches nothing new and shows nothing twice. A check of who is online can be added later if the quota or the battery asks for it.

FCM is called through its HTTP v1 API with a token for the service account, which the standard OAuth2 library for Google builds from the key file. This avoids the Firebase Admin SDK, which is a large dependency for one call, and the call is easy to test against a local server. `FIREBASE_CREDENTIALS_FILE` names the key file and the project id comes from it. With the setting empty no push is sent and the routes for the tokens answer that pushes are off, the same way the uploads behave without a bucket. The key lives only on the server and is never in the repository or the image.

Encrypting the payload and decrypting it on the phone, as Telegram does, was left out because it needs a key for each device and a way to rotate it, and it can be added later without changing the rest. Holding a socket open in a foreground service was rejected because of the battery and the limits that Android puts on it.

## Consequences

- Google sees that a device got a push at a time, and the two ids. It does not see the text, the sender or the child.
- A push is not guaranteed. A phone whose app was force stopped, or that saves battery hard, may not wake the app, so the app also catches up with `after` whenever it opens.
- The app needs the Firebase library, the Google services plugin, the permission to show notifications on Android 13 and later and a notification channel. The Firebase project is created by the owner, and the key never goes through the chat or the repository.
- The privacy page has to say what goes to Google and what does not, and the table of tokens is personal data of the account that goes with the account.
- An account that signs in on a second phone has two tokens and both ring. Signing out removes the token of that phone only.
- Every message makes one push for each other person, also for people who are looking at the app. FCM has no per message price, and its quota is far above what one school sends.
