-- Google Sign-In links an existing or newly registered user by verified Google subject identifier (sub).
ALTER TABLE users ADD COLUMN google_sub VARCHAR(255);

CREATE UNIQUE INDEX idx_users_google_sub ON users (google_sub) WHERE google_sub IS NOT NULL AND deleted_at IS NULL;
