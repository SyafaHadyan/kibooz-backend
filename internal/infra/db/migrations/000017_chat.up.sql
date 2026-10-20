-- 000017 chat
-- A teacher and a parent talk in a direct conversation. There is one row for each pair of accounts, so a parent has one
-- thread with each teacher whatever the number of children. The smaller id of the two is always user_low_id, which lets
-- the unique key catch the pair in either order.
CREATE TABLE chat_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_low_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_high_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CHECK (user_low_id < user_high_id),
    UNIQUE (user_low_id, user_high_id)
);

CREATE INDEX idx_chat_conversations_high ON chat_conversations(user_high_id);

-- The id only grows and is the cursor of a conversation. Sending takes a lock on the conversation row first, so the
-- messages of one conversation get their ids in the order they commit and a client that holds a cursor cannot skip one.
-- A deleted message keeps its row with an empty body, so the thread shows a placeholder.
CREATE TABLE chat_messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body TEXT NOT NULL CHECK (char_length(body) <= 4000),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    deleted_at TIMESTAMP WITH TIME ZONE,
    CHECK ((deleted_at IS NULL AND char_length(body) > 0) OR (deleted_at IS NOT NULL AND body = ''))
);

CREATE INDEX idx_chat_messages_conversation ON chat_messages(conversation_id, id DESC);

-- The last message each member has read, which gives the unread count and lets the other side show a read mark.
CREATE TABLE chat_reads (
    conversation_id UUID NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_message_id BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);
