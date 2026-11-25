-- An account that waits for the code of its email is marked, and every account that exists today counts as verified. The flag is
-- false for old rows and for a row that an older copy inserts during a rolling update, so the new rule cannot lock them out.
ALTER TABLE users ADD COLUMN email_verification_pending BOOLEAN NOT NULL DEFAULT FALSE;

-- One live code for each account that waits. The code itself is never stored, only a keyed hash of it.
CREATE TABLE email_verifications (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    code_hash VARCHAR(64) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    attempts SMALLINT NOT NULL DEFAULT 0,
    last_sent_at TIMESTAMP WITH TIME ZONE NOT NULL,
    -- the sends of the current hour, so one address cannot be used to mail somebody over and over
    window_started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    sends_in_window SMALLINT NOT NULL DEFAULT 1
);
