-- a message that only has media cannot exist under the old rule, so those rows go. Their files stay in the bucket and the
-- lifecycle of the bucket does not touch them, so remove them by hand if this is ever run on real data.
DELETE FROM chat_messages WHERE deleted_at IS NULL AND body = '';

ALTER TABLE chat_messages DROP CONSTRAINT IF EXISTS chat_messages_content;

ALTER TABLE chat_messages ADD CONSTRAINT chat_messages_check CHECK (
    (deleted_at IS NULL AND char_length(body) > 0) OR (deleted_at IS NOT NULL AND body = '')
);

ALTER TABLE chat_messages DROP CONSTRAINT IF EXISTS chat_messages_media_shape;

ALTER TABLE chat_messages
    DROP COLUMN media_size,
    DROP COLUMN media_type,
    DROP COLUMN media_kind,
    DROP COLUMN media_key;
