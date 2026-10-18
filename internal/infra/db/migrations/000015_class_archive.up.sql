-- A class that nobody teaches any more and that has no active child is archived instead of staying behind with a join code
-- that still answers. Archived classes keep their rows and their code, so the code can never be given to another class.

ALTER TABLE classes ADD COLUMN archived_at TIMESTAMPTZ;
