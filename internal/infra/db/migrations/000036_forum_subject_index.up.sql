-- Create partial index for class/subject forum listing in a separate transaction after constraint validation.
CREATE INDEX idx_forum_threads_class_subject ON forum_posts(class_id, subject_code, created_at DESC, id DESC) WHERE parent_id IS NULL;
