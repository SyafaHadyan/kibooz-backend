DROP INDEX IF EXISTS idx_class_teachers_owner;

ALTER TABLE class_teachers DROP COLUMN IF EXISTS is_owner;
