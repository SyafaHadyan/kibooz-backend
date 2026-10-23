-- fails when two active children share a NISN, and the children that lost their NISN to a verification all share the empty one, so
-- give those children a number or remove them first, and note that every NISN is unverified again after the up migration
DROP INDEX idx_students_nisn;
DROP INDEX students_nisn_verified_key;
CREATE UNIQUE INDEX students_nisn_key ON students (nisn) WHERE deleted_at IS NULL;

ALTER TABLE students DROP CONSTRAINT students_nisn_verified_not_empty;
ALTER TABLE students DROP COLUMN nisn_verified_at;
