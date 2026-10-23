-- The hourly purge of refresh tokens looks for two kinds of rows, the tokens that expired unused and the used tokens whose session
-- reached its longest lifetime. No index served either condition, so every batch read the whole table. A partial index for each kind
-- keeps the two apart, so the purge reads the range of old rows and not the whole table. The index of used tokens holds every used
-- token, and a refresh that sets used_at now moves a row from one index to the other, which is the price of the purge.
CREATE INDEX idx_refresh_tokens_unused_expiry ON refresh_tokens (expires_at) WHERE used_at IS NULL;
CREATE INDEX idx_refresh_tokens_used_session_start ON refresh_tokens (session_started_at) WHERE used_at IS NOT NULL;
