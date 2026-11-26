-- the number of codes that were mailed to an account in all, so the lifetime cap of codes can be checked
ALTER TABLE email_verifications ADD COLUMN sends_total INTEGER NOT NULL DEFAULT 0;
-- when an account that still waits for its code signed in because the verification was off, so it is never removed for waiting
ALTER TABLE users ADD COLUMN signed_in_unverified_at TIMESTAMPTZ;
