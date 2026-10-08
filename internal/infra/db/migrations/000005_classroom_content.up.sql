-- A teacher adds learning videos to a class as https addresses, and the class forum holds threads and replies.
-- A thread is a forum post with a title and no parent, a reply is a post without a title that points at a thread of the same class.

CREATE TABLE learning_videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    class_id UUID NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    added_by_guru_id UUID NOT NULL REFERENCES gurus(id) ON DELETE RESTRICT,
    title VARCHAR(150) NOT NULL,
    description TEXT,
    video_url TEXT NOT NULL,
    thumbnail_url TEXT,
    duration_seconds INTEGER CHECK (duration_seconds IS NULL OR duration_seconds > 0),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_learning_videos_class ON learning_videos(class_id, created_at DESC, id DESC);

CREATE TABLE forum_posts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    class_id UUID NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    parent_id UUID,
    author_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(150),
    body TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (id, class_id),
    -- the composite key keeps a reply in the class of its thread
    FOREIGN KEY (parent_id, class_id) REFERENCES forum_posts(id, class_id) ON DELETE CASCADE,
    CHECK ((parent_id IS NULL) = (title IS NOT NULL))
);

CREATE INDEX idx_forum_threads_class ON forum_posts(class_id, created_at DESC, id DESC) WHERE parent_id IS NULL;
CREATE INDEX idx_forum_replies_thread ON forum_posts(parent_id, created_at, id) WHERE parent_id IS NOT NULL;
