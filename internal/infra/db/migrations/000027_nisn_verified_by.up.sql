-- Who vouched for a NISN, so a verification that took a number from a real child can be traced to the teacher who made it. The column
-- stays empty for a NISN that is not verified. It is cleared with the verification, and it keeps the first teacher when another
-- teacher verifies again. The table is small and the column is empty, so the foreign key is checked without a long lock.
-- squawk-ignore adding-foreign-key-constraint
ALTER TABLE students ADD COLUMN nisn_verified_by UUID REFERENCES gurus (id) ON DELETE SET NULL;
