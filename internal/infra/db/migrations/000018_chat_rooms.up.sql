-- 000018 chat class rooms
-- A class room is the second kind of conversation, one for each class. It has no pair of accounts. Its members are not stored,
-- they are the teachers of the class and the parents with an active child in it, so a person who joins or leaves the class
-- gains or loses the room without any bookkeeping here. A member sees only the messages sent after they joined, which the
-- queries work out from class_teachers.created_at and students.created_at.
ALTER TABLE chat_conversations
    ADD COLUMN kind VARCHAR(10) NOT NULL DEFAULT 'direct' CHECK (kind IN ('direct', 'class')),
    ADD COLUMN class_id UUID REFERENCES classes(id) ON DELETE CASCADE,
    ALTER COLUMN user_low_id DROP NOT NULL,
    ALTER COLUMN user_high_id DROP NOT NULL;

ALTER TABLE chat_conversations ADD CONSTRAINT chat_conversations_shape CHECK (
    (kind = 'direct' AND class_id IS NULL AND user_low_id IS NOT NULL AND user_high_id IS NOT NULL)
    OR (kind = 'class' AND class_id IS NOT NULL AND user_low_id IS NULL AND user_high_id IS NULL)
);

CREATE UNIQUE INDEX uq_chat_conversations_class ON chat_conversations(class_id) WHERE class_id IS NOT NULL;
