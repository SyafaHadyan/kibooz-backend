# Chat socket

The chat socket pushes what happens in the conversations of the signed in account, so an app does not have to poll. It is only a shortcut. Every message is also in PostgreSQL and in `GET /chat/conversations/{conversationId}/messages`, so an app that was offline, or that lost an event, catches up with the cursor. Sending, deleting and marking as read stay REST requests, because they keep the limits, the idempotency keys and the checks of every other route. The reasons are in [ADR 0005](adr/0005-chat-rest-and-websocket.md).

## Connect

Open `GET /api/v1/chat/ws` with the WebSocket handshake and the header `Authorization: Bearer <access token>`. The token is never put in the address, because addresses end up in logs. The roles are `GURU` and `WALI`, like the other chat routes.

| Answer | Meaning |
|:---|:---|
| `101` | The connection is a WebSocket now |
| `401` | The token is missing, wrong or over, refresh it and try again |
| `426` with `UPGRADE_REQUIRED` | The request was not a WebSocket handshake |
| `503` with `CHAT_SOCKETS_FULL` | This copy of the API holds as many sockets as it may, or is shutting down. Connect again after a few seconds and the proxy sends the request to a copy with room |

## Frames from the server

Every frame is one JSON text frame with a `type`. The `conversationId` is the conversation the event is about.

| `type` | When | Other fields |
|:---|:---|:---|
| `ready` | Once, right after the connection opens | `expiresAt`, when the access token ends and the server closes the socket |
| `message` | A message was sent to a conversation of the account, its own messages too, so its other devices see them | `message`, the same shape as a message of the REST routes, with the sender and the media |
| `message.deleted` | A message was deleted, the first time only | `messageId` |
| `read` | A person read a conversation up to a message. In a private conversation both people hear it, in a class room only the other devices of the reader | `userId` and `lastReadMessageId` |
| `typing` | The other person of a private conversation started or stopped typing | `userId` and `typing` |
| `resync` | The server noticed that events may have been lost on their way, for example while Redis was down. It comes with the event that revealed the gap, at most once every 5 seconds for a copy, and names no conversation | none |

A `message` frame carries signed addresses for its photo or video, which work until `media.expiresAt`, like the ones of the REST routes.

## Frames from the app

The app sends one kind of frame, a hint that the person is typing in a private conversation.

```json
{"type": "typing", "conversationId": "6f1c1a52-0c55-4d6c-a4a9-8f54a39bd1a8", "typing": true}
```

Send it when the person starts typing and about every three seconds while they go on, and send `typing: false` when they stop or send. The server tells only the other person. A class room has no typing, because everyone in the class would see it. A frame about a conversation that is not yours, a closed conversation or a room is dropped without an answer. The server acts on one frame a second for each conversation and closes a socket that sends more than about five frames a second for long, or that keeps sending frames it cannot read, with the code `1008`. Everything else the app writes goes through REST.

## Staying connected

The server pings every 25 seconds and closes a socket that shows no sign of life for 60 seconds. A WebSocket client answers the pings by itself, so an idle app stays connected and a phone that went away is dropped. When a socket closes, the app connects again with a short wait that grows, and then asks for what it missed.

| Close code | Meaning | What the app does |
|:---|:---|:---|
| `1001` | The server is going away, for example during a deploy | Connect again, and the proxy sends it to another copy |
| `4001` | The access token ended | Refresh the token, connect again and catch up |
| `1008` | The app sent too many frames or frames that make no sense | Fix the app |
| `1013` | The copy could not take the socket right now | Connect again in a few seconds |

An account keeps at most 5 sockets (`CHAT_MAX_SOCKETS_PER_USER`). A sixth connection closes the oldest one, so a phone that reconnects before the server noticed the old connection is gone is never turned away. A copy holds at most `CHAT_MAX_SOCKETS` sockets, 1000 by default. A socket that reads too slowly to keep up with its events is closed, and the app that connects again catches up.

## Catching up

After a connection opens (right after the `ready` frame), after a `resync` frame, and after the app was in the background, ask `GET /chat/conversations/{conversationId}/messages?after=<cursor>` for each open conversation, and refresh `GET /chat/conversations` for the unread counts and the last messages. The ids only grow inside a conversation, but they are not consecutive, so an app cannot tell from the ids of the frames whether it missed one.

Keep the cursor of each conversation from what the REST route returned, and do not move it forward because a `message` frame arrived. Suppose message 5 was lost and message 6 arrived by the socket. If the app then used 6 as the cursor, it would ask for what comes after 6 and never get 5. Show the message of a frame at once, but ask for `after=<cursor>` when the socket opens, when a `resync` arrives and when the app returns to the foreground, and set the cursor to the highest id of the answer. A message that arrives twice, by the socket and by the route, has the same id, so the app shows it once. The id in the answer of `POST .../messages` is not a cursor either, because messages of other people that were sent before it may be missing. When a `resync` arrives, wait a random time of up to a few seconds before the requests, so that thousands of phones do not ask at the same moment, and do the work once for several frames that come close together.

A deleted message shows up again as a placeholder with `deleted` set, so an app that missed the `message.deleted` frame still shows the right thing once it reloads the window.

## How events reach the other copies

The API runs as more than one copy, and a person's socket may be on a different copy than the request that sent the message. Each copy delivers an event to its own sockets and publishes it to a Redis channel, `chat:events`, which the other copies listen to. Redis only makes delivery faster. When it is down, the sockets on the copy that handled the request still hear about it, the others do not, and their apps get the message the next time they ask. Every copy numbers what it publishes, so a copy that sees a number skipped, for example after Redis came back, sends a `resync` frame to all of its sockets, and the apps ask for what they missed without waiting for the next time. A copy sends it at most once every 5 seconds, and the metric `kibooz_chat_event_gaps_total` counts the gaps that it found. The metrics `kibooz_chat_sockets`, `kibooz_chat_events_delivered_total`, `kibooz_chat_sockets_dropped_total` and `kibooz_chat_sockets_refused_total` show how many sockets are open, what was written to them, why the server closed one and how many were turned away.

## Reverse proxy

A proxy has to pass the upgrade and keep an idle connection open for longer than the ping interval. For nginx, give the route its own location and a read timeout above 60 seconds.

```nginx
location /api/v1/chat/ws {
    proxy_pass http://kibooz_api;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;
}
```

The `upstream` of the two copies is the same as for the rest of the API. A copy that is stopped during a deploy sends a going away frame to its sockets, and they reconnect through the proxy to the other copy. Cloudflare passes WebSockets without a setting. The login and register flood limit of the proxy does not touch this route, so do not add a request rate limit to it, because the socket is one long request.
