DROP INDEX IF EXISTS uq_chat_conversations_class;

ALTER TABLE chat_conversations DROP CONSTRAINT IF EXISTS chat_conversations_shape;

-- the rooms have no pair of accounts, so they and their messages go before the columns come back as required
DELETE FROM chat_conversations WHERE kind = 'class';

ALTER TABLE chat_conversations
    DROP COLUMN class_id,
    DROP COLUMN kind,
    ALTER COLUMN user_low_id SET NOT NULL,
    ALTER COLUMN user_high_id SET NOT NULL;
