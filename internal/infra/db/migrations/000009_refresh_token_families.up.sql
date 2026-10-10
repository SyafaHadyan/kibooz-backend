-- A refresh token belongs to a session family, one for every sign in. Using a token keeps its row and marks it used, so
-- a used token that is shown again is recognized as a replay and the whole family ends. session_started_at is the time
-- of the sign in, and it caps how long refreshing can keep the session alive.

ALTER TABLE refresh_tokens
    ADD COLUMN family_id UUID,
    ADD COLUMN session_started_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN used_at TIMESTAMP WITH TIME ZONE;

-- a token that exists now is the only one of its family and started its session when it was created
UPDATE refresh_tokens SET family_id = id, session_started_at = created_at;

ALTER TABLE refresh_tokens
    ALTER COLUMN family_id SET NOT NULL,
    ALTER COLUMN session_started_at SET NOT NULL;

CREATE INDEX idx_refresh_tokens_family ON refresh_tokens(family_id);
