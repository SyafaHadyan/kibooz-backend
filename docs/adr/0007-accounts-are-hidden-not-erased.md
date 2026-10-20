# 0007 Accounts are hidden, not erased

Status: Accepted, 2026-10-21. This record was written after the fact, the rules had been in place since the soft delete migration.

## Context

A teacher or a parent can delete an account. The rows of an account are tied to a lot of other data. A mood belongs to a child and a teacher, a trash claim adds to the points of a ranking, a forum post sits in a thread that others reply to, and the final standings of a semester are copied into a history table. Removing the rows would either cascade through all of it or leave rows that point at nothing, and a crash in the middle of it would leave a half deleted account.

The photos are the other half. They live in a bucket, outside the database, so a deletion cannot be one transaction.

## Decision

Deleting an account hides it. The user, the profile row and every child of a parent get a `deleted_at` time, and from then on every query, login and refresh token ignores them. The unique rules on email, NIP and NISN only cover active rows, so an email or a NISN can be used again after a deletion.

What the account made stays in the database. Moods, scans, guidance records, forum posts and chat messages are kept, and a person who was deleted shows as `Deleted account` with no photo. The class ranking is renumbered, and the cached leaderboard is cleared.

What is stored in the bucket does not stay. The avatar of the user, the avatars of the children and the photos of their trash scans have their addresses cleared, and their object keys go into the `file_cleanups` table in the same transaction that hides the account. A job then deletes the files and takes the keys off the table, so a crash cannot make the API forget a file and a failure of the bucket only delays the deletion.

A teacher who is the only teacher of a class with active children has to name another registered teacher in `successorEmail`, who takes the class over. A class that nobody else teaches and that has no active child is archived instead, which keeps its rows and its join code so the code is never given to another class. A parent and a teacher remove a child through the same function, `internal/childremoval`, so the steps do not differ by who asks.

## Consequences

- Nothing in the code erases the rows of a deleted account, and no route does it. A request to erase everything is a manual job in the database, and [privacy.md](../privacy.md) says so.
- Counts and history stay correct after a deletion, and a thread keeps making sense for the people who are still in it.
- Every new table that points at a user has to decide what a deleted person looks like, and the answer so far has been to keep the row and show `Deleted account`.
- A deleted email can be registered again, and the new account is a different one with none of the old data.
