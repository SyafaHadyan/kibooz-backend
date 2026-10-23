-- fails when two active children share a NISN or one has an empty NISN, resolve those rows first
DROP INDEX idx_students_nisn;
DROP INDEX students_nisn_verified_key;
CREATE UNIQUE INDEX students_nisn_key ON students (nisn) WHERE deleted_at IS NULL;

ALTER TABLE students DROP CONSTRAINT students_nisn_verified_not_empty;
ALTER TABLE students DROP COLUMN nisn_verified_at;
