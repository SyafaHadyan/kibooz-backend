-- 000019 chat media
-- A message can carry one photo or one video, which lives in the private bucket under the key in media_key. A message may now
-- have no text when it has media. Deleting a message clears its media columns and the file goes on the cleanup queue.
ALTER TABLE chat_messages
    ADD COLUMN media_key TEXT,
    ADD COLUMN media_kind VARCHAR(10) CHECK (media_kind IN ('photo', 'video')),
    ADD COLUMN media_type VARCHAR(100),
    ADD COLUMN media_size BIGINT CHECK (media_size > 0);

-- the four columns are all set or all empty
ALTER TABLE chat_messages ADD CONSTRAINT chat_messages_media_shape CHECK (
    (media_key IS NULL AND media_kind IS NULL AND media_type IS NULL AND media_size IS NULL)
    OR (media_key IS NOT NULL AND media_kind IS NOT NULL AND media_type IS NOT NULL AND media_size IS NOT NULL)
);

-- the old rule asked for text on every message that is not deleted, so it is replaced by one that also accepts media. It was
-- created without a name, so it is found by what it says.
DO $$
DECLARE
    old_rule text;
BEGIN
    SELECT conname INTO old_rule FROM pg_constraint
    WHERE conrelid = 'chat_messages'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%deleted_at IS NULL%char_length%';

    IF old_rule IS NULL THEN
        RAISE EXCEPTION 'the check of chat_messages that ties the text to deleted_at was not found';
    END IF;

    EXECUTE format('ALTER TABLE chat_messages DROP CONSTRAINT %I', old_rule);
END $$;

ALTER TABLE chat_messages ADD CONSTRAINT chat_messages_content CHECK (
    (deleted_at IS NULL AND (char_length(body) > 0 OR media_key IS NOT NULL))
    OR (deleted_at IS NOT NULL AND body = '' AND media_key IS NULL)
);
