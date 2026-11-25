-- the number of codes that were mailed to an account in all, so the lifetime cap of codes can be checked
ALTER TABLE email_verifications ADD COLUMN sends_total INTEGER NOT NULL DEFAULT 0;
