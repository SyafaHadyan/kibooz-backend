-- Google Sign-In finds the account by the verified Google subject identifier (sub), and an account links or unlinks it from its settings.
ALTER TABLE users ADD COLUMN google_sub VARCHAR(255);

CREATE UNIQUE INDEX idx_users_google_sub ON users (google_sub) WHERE google_sub IS NOT NULL AND deleted_at IS NULL;
