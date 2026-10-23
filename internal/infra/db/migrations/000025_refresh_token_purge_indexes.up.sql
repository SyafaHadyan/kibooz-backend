-- The hourly purge of refresh tokens looks for two kinds of rows, the tokens that expired unused and the used tokens whose session
-- reached its longest lifetime. No index served either condition, so every batch read the whole table. A partial index for each kind
-- holds only the rows that its condition reads, so the purge finds the old rows without a scan.
CREATE INDEX idx_refresh_tokens_unused_expiry ON refresh_tokens (expires_at) WHERE used_at IS NULL;
CREATE INDEX idx_refresh_tokens_used_session_start ON refresh_tokens (session_started_at) WHERE used_at IS NOT NULL;
