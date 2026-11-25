-- when the mail of a code is tried again after it failed, and how many times it failed since the last mail that went out
ALTER TABLE email_verifications ADD COLUMN retry_at TIMESTAMPTZ;
ALTER TABLE email_verifications ADD COLUMN mail_failures INTEGER NOT NULL DEFAULT 0;
