-- Notifications keep per-user in-app updates emitted when mood logs, guidance, moves, or classroom content happen.

CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(40) NOT NULL,
    title VARCHAR(150) NOT NULL,
    body TEXT NOT NULL,
    reference_type VARCHAR(40),
    reference_id UUID,
    read_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notifications_user_created ON notifications(user_id, created_at DESC, id DESC);
-- the unread list and the unread count read only the unread rows of one user, newest first
CREATE INDEX idx_notifications_user_unread ON notifications(user_id, created_at DESC, id DESC) WHERE read_at IS NULL;
-- the hourly purge of the notifications older than the retention finds them without reading the whole table
CREATE INDEX idx_notifications_created ON notifications(created_at);
