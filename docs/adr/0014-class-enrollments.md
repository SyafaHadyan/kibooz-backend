# 0014 A child moves to another class through an enrollment

Status: Proposed

## Context

Today a child belongs to one class for good. The `students` row holds the class, the parent creates the child with a join code and a NISN, and no route changes the class. A child who goes to next year's class has to be registered again, and the NISN is unique among active children, so that is impossible without deleting the child first and losing the history. A teacher can also close joining and remove a child, but the history of the child stays with the class that wrote it.

The owner decided on 2026-10-21 how a move should work. This record writes those decisions down so the design can be reviewed before any code is written.

## Decision

A move is an enrollment, a record that says that a child asks to be, is, or was in a class.

- **Enrollments.** A new table holds one row for each time a child asks for, joins or leaves a class, so a child that comes back to a class gets a new row, with a status of `pending`, `active`, `ended` or `rejected`, the time of each step and the teacher who decided. The class of the child in `students` always names the class of the one `active` enrollment, so the leaderboard and every other query keep working as they do now. A child has at most one active enrollment, enforced by a partial unique index, and the rule can be relaxed later if a child may be in two classes.
- **Who approves.** A first registration with a join code stays instant, as it is now. A move, which means the child already has an active enrollment in another class, creates a `pending` enrollment in the new class, and any teacher of the new class accepts or rejects it. A parent can cancel a pending request.
- **What a move does.** Accepting ends the old enrollment and activates the new one in one transaction. The class, the all-time points and the semester points of the child all start from zero in the new class. The points, the rank and the number of scans of the old class are saved on the ended enrollment, so they are never lost and can be listed as the previous classes of the child.
- **History.** A teacher of the new class may read the old history of the child, read only. The decision is to share everything about the child, which means the stats, the moods and the notes that the old teacher wrote, and nothing about the other children, the forum or the chat of the old class. This changes what `docs/privacy.md` says and has to be written there before it ships.
- **Rejected requests.** A rejection only touches the enrollment. There is no limit on how often a parent can ask again, so the rate limiters of the account are the only brake.
- **Archive and restore.** The owner of a class archives it, whatever number of children it has, and any teacher of the class restores it. An archived class is read only, with its join code out of use, its leaderboard frozen and its enrollments kept, while a child can still move out of it. A restore gives the class a new join code and counts toward the 20 classes of a teacher. Before this record the only way to archive a class was that its only teacher, with no active child, deleted the account, and then nobody was left to restore it.
- **Registration.** The NISN stays required at registration. The idea that preschool children may have none is unverified, and the unique NISN is what stops one child being registered twice.

## Consequences

- A move touches two classes, so it takes the class locks of both in the order of their ids, which avoids a deadlock between two moves in opposite directions. Both rankings are renumbered and both leaderboard caches are cleared, as a removal does today.
- The ended enrollment keeps the saved numbers, so the semester job and `leaderboard_history` need no change, because the old class already saved its final standings when the semester ended.
- Every list of children, every count of a class and the caps on children per class have to read the active enrollments or keep `students.class_id` in step, so the migration backfills one active enrollment for every existing child.
- Teachers read data that another teacher wrote, which is the main privacy cost, and it is why the decision is recorded here and in the privacy page.
- New routes and error codes are needed (request a move, list and decide requests, cancel, previous classes of a child, restore a class), all documented in `openapi.yaml`, the error table and the client guide, and the app needs a screen for each.

## Open points for review

- Settled while building the history route. The history of the old class is visible to the new teacher only after the move is accepted, through `GET /guru/classes/{classId}/students/{studentId}/history`, and `docs/privacy.md` says so.
- Whether a parent is told by push when a request is decided, which reuses the wake-up push of ADR 0013.
- Settled while building the move routes. A request that waits is rejected by itself when the new class is archived or closes joining, through a trigger on `classes`, and the parent sees it as rejected and can ask again. A child has at most one request that waits, and a teacher of the new class decides it.
- Settled while building the previous classes route. A parent sees the classes that their own child left, with the dates and the points, rank and scans saved when the child left, through `GET /wali/children/{studentId}/previous-classes`.
