# 0005 Chat sends over REST and receives over a WebSocket

Status: Accepted, 2026-10-21

## Context

Teachers and parents need to talk to each other. The forum covers posts about a class, but not a private talk about one child or a quick answer. The feature is meant to feel like a messaging app, with live delivery, unread counts, photos and videos, and typing indicators.

Several facts of this API shape the design. The messages are about children, so access has to follow the rules the rest of the API uses, where a parent never sees another parent's data. The API runs as two copies behind nginx, each capped at half a CPU, so nothing may live only in the memory of one process. Every route is described in `openapi.yaml` and the tests check each response against it. The phone loses its connection often, and the app repeats queued actions until one works.

## Decision

There are two kinds of conversation. A direct conversation is one row for each pair of accounts, so a parent has one thread with each teacher whatever the number of children. A class room is one for each class, and its members are the teachers of the class and the parents with an active child in it. A person starts a direct conversation only while they share a class with the other person, and an archived class still counts as shared, so archiving a class does not close the direct conversations of its teachers and parents. When they no longer share a class, for example after a child moved out, history stays readable, but sending stops. The room of an archived class is the one that closes for sending. A member of a room sees only the messages sent after they joined.

Messages live in PostgreSQL and each has a number that only grows, which is also the cursor ([0001](0001-postgresql-decides-redis-helps.md)). Sending is a normal REST request with an `Idempotency-Key` ([0002](0002-idempotency-middleware.md)), so the limits, the validation and the contract tests apply to it like to every other route. Receiving and typing use one WebSocket for each app. The server pushes new and deleted messages and read marks, and the app sends typing frames. Redis pub/sub carries events between the API copies. Redis only makes delivery faster. When it is down, or the socket is closed, the app polls and catches up with the cursor.

A message holds text of up to 4000 characters and at most one photo or video. A photo is at most 4 MiB and a video at most 25 MiB. A photo goes through the API, like an avatar, so the server can check it and cut the metadata, such as the location, out of the file. A video goes straight to object storage with a presigned address, the same way class videos work, because it is too large to pass through a copy of half a CPU. Either way it waits under `pending/` and the server copies the file on send. The server only accepts a key that it issued to that sender for that conversation, so an arbitrary key or address from the app is rejected before any copy or signed read address, and a member of the conversation is the only one who gets a read address. Chat media goes to a private bucket and the API hands members short-lived signed addresses to read it. The public bucket stays as it is for avatars, banners and class videos. A bucket exists for each access policy, and a feature uses a prefix inside it.

A message stays after an account is deleted, with the sender shown as a deleted account, and so does its media. Media is kept without an end date. An author can delete a message, which leaves a placeholder. There is no report or block, no limit on how often someone sends beyond the global limit of each user, and typing shows in private threads only. Push notifications are not part of this decision and come later with Firebase.

Server-Sent Events were the other candidate for receiving. They need no new package and stay inside `openapi.yaml`, but typing would cost a request every few seconds and the WebSocket is the usual choice for chat. A WebSocket for sending as well was rejected, because the limits, the idempotency and the validation would have to be built again for the socket.

## Consequences

- The socket uses `github.com/gofiber/contrib/v3/websocket`. Version 1.2.5 needs the Fiber and fasthttp versions already in use. It also needs `github.com/gofiber/utils/v2` 2.5.0, where the repository has 2.4.1, so adding it moves that one module forward in the first pull request that uses it. Renovate keeps the package current after that.
- A WebSocket cannot be described in OpenAPI. It gets its own page in `docs/` and its own end to end tests, and the server counts open sockets, messages and errors itself. It sends a ping every 25 seconds, closes a socket that has sent no pong for 60 seconds, so an idle app that answers the pings stays connected, and closes one whose access token has expired so the app can refresh and reconnect.
- Replacing an API copy cuts its sockets. The apps reconnect and catch up with the cursor, so no message is lost.
- A closed app hears nothing until push notifications exist. The app shows new messages when it opens.
- The private bucket is a second R2 bucket with an API token that can use it. Signed addresses change on every request, so the app caches photos by the id of the attachment and not by the address. Access depends on membership, so the app keeps that cache for the signed-in account only and clears it on logout and on an account change.
- Parents cannot message each other in private, only in the class room where the teachers see everything.
- Messages and media only grow. Storage is cheap, but a retention rule needs its own decision later.
- `openapi.yaml` describes the REST routes and their schemas. The WebSocket frames are described on the docs page of the socket and the tables in the migration files and the database docs, each when its pull request lands.
