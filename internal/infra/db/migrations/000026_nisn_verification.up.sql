-- A teacher can mark the NISN of a child of the class as verified. The unique rule only holds for the verified ones, so a parent
-- whose NISN was taken by someone who made it up is not shut out, and a teacher who verifies a NISN takes it from every unverified
-- holder. The child that loses it keeps its record with an empty NISN and has to enter a real one, which is why the column stays
-- NOT NULL. A verified row always has a NISN.
ALTER TABLE students ADD COLUMN nisn_verified_at TIMESTAMPTZ;
ALTER TABLE students ADD CONSTRAINT students_nisn_verified_not_empty CHECK (nisn_verified_at IS NULL OR nisn <> '');

DROP INDEX students_nisn_key;
CREATE UNIQUE INDEX students_nisn_verified_key ON students (nisn) WHERE deleted_at IS NULL AND nisn_verified_at IS NOT NULL;

-- the registration, the change of a NISN and the verification look for the children that hold a number
CREATE INDEX idx_students_nisn ON students (nisn) WHERE deleted_at IS NULL AND nisn <> '';
