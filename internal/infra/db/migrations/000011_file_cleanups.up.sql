-- A file that could not be removed from the bucket when its owner deleted the account waits here until a later try works,
-- because the rows that pointed at it are already cleared and nothing else would ever find it again.

CREATE TABLE file_cleanups (
    object_key TEXT PRIMARY KEY,
    attempts INTEGER NOT NULL DEFAULT 0,
    queued_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    -- a file that keeps failing waits longer between tries, so it cannot crowd newer files out of a sweep
    next_try_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX idx_file_cleanups_next_try ON file_cleanups(next_try_at);
