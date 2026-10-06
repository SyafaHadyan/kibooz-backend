-- Existing rows keep the value they were stored with, only the default for new classes changes.
ALTER TABLE classes ALTER COLUMN grade_level SET DEFAULT 'Class A';
