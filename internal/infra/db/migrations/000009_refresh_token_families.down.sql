-- the used tokens only stay to recognize a replay, and without the column they would look like tokens that can be used
DELETE FROM refresh_tokens WHERE used_at IS NOT NULL;

DROP INDEX IF EXISTS idx_refresh_tokens_family;

ALTER TABLE refresh_tokens
    DROP COLUMN used_at,
    DROP COLUMN session_started_at,
    DROP COLUMN family_id;
