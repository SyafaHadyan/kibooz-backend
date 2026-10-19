-- A teacher of a class can stop it from taking new children without replacing the join code. Every class starts open, as
-- all of them are today.

ALTER TABLE classes ADD COLUMN join_open BOOLEAN NOT NULL DEFAULT TRUE;
