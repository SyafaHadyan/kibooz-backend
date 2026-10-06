-- fails when a deleted row shares its email, NIP or NISN with an active one, resolve those rows first
DROP INDEX students_nisn_key;
DROP INDEX gurus_nip_key;
DROP INDEX users_email_key;

ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
ALTER TABLE gurus ADD CONSTRAINT gurus_nip_key UNIQUE (nip);
ALTER TABLE students ADD CONSTRAINT students_nisn_key UNIQUE (nisn);

ALTER TABLE students DROP COLUMN deleted_at;
ALTER TABLE walis DROP COLUMN deleted_at;
ALTER TABLE gurus DROP COLUMN deleted_at;
ALTER TABLE users DROP COLUMN deleted_at;
