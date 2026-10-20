# 0010 Files live in buckets, one bucket for each access policy, and uploads wait under `pending/`

Status: Accepted, 2026-10-21. This record was written after the fact. The public bucket and the video flow are in production, the private bucket is built together with chat media ([0005](0005-chat-rest-and-websocket.md)) and stays off while its setting is empty.

## Context

The app shows avatars, trash photos and class videos, and chat will carry photos and videos between a teacher and a parent. A photo of a child must never be readable by a stranger who guesses an address, while the guidance banners and videos are meant for everyone in the app. A video is far larger than a photo, so sending it through two API copies of half a CPU each would use up their memory and their time. A file that was uploaded but never attached to anything must not stay forever, and a bucket that serves public files must not hold anything private by mistake.

## Decision

The API stores files in an S3 compatible bucket (Cloudflare R2) and the bucket is optional. Without its settings every upload route is off, and the rest of the API works.

There is one bucket for each access policy and one prefix for each feature. The public media bucket serves its objects to anyone through its own domain (`S3_PUBLIC_URL`), because the app plays and shows them from there. The private bucket is never public, and its files are read through signed addresses that expire. The names follow `kibooz-<environment>-<purpose>`, so production and staging never share a bucket and a backup bucket is a third purpose. One access key may cover the two production buckets, and a backup bucket gets a key of its own.

Photos go through the API. The server checks their size and type by content and cuts the metadata out of the file without decoding it, as described in the [design notes](../design.md), and then writes it to the bucket. Videos do not go through the API. The app asks for a signed upload address, sends the video straight to the bucket under `pending/`, and then tells the API to attach it, which copies the file to its final prefix.

Everything that is uploaded and not yet attached sits under `pending/`, and a lifecycle rule of the bucket expires that prefix after one day. The rule must never cover `videos/`. A file that a deleted account left behind is queued in the `file_cleanups` table and removed by a job ([0007](0007-accounts-are-hidden-not-erased.md)).

`/healthz` writes and removes a small probe object. A bucket that refuses it turns the status to `degraded` and never to `down`, so the API keeps serving.

## Consequences

- A video never passes through the API, so its size does not cost API memory, and the limit on its size is enforced by the signed address and checked again when it is attached.
- A bucket and its lifecycle rule are settings outside the repository. A rule with a wrong prefix deletes real videos, so the docs say it in the operations table and the troubleshooting list.
- Chat media can be private without changing how the public media works, and a feature that needs a third policy gets a third bucket and not a mixed one.
- Every new feature that stores a file has to say what its prefix is, which bucket it uses and who cleans it up.
- The probe makes a write on every health check, and a slow bucket shows up as a slow `/healthz` or a `degraded` status and not as an outage.
