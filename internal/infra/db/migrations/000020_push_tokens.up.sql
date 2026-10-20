-- 000020 push tokens
-- The token that Firebase gives an installed app, so the server can wake it when a chat message arrives. One row for each device,
-- and a token belongs to one account at a time, so a phone that signs in as somebody else moves its token to that account.
CREATE TABLE push_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE CHECK (char_length(token) BETWEEN 20 AND 4096),
    platform VARCHAR(10) NOT NULL DEFAULT 'android' CHECK (platform IN ('android')),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX idx_push_tokens_user ON push_tokens(user_id, updated_at DESC);
