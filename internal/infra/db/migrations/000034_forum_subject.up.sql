-- Optional curriculum subject code on top-level forum threads (NULL means general class discussion).
ALTER TABLE forum_posts ADD COLUMN subject_code VARCHAR(20);
ALTER TABLE forum_posts ADD CONSTRAINT forum_posts_subject_reply_check CHECK (parent_id IS NULL OR subject_code IS NULL) NOT VALID;
ALTER TABLE forum_posts VALIDATE CONSTRAINT forum_posts_subject_reply_check;
ALTER TABLE forum_posts ADD CONSTRAINT forum_posts_subject_code_check CHECK (
    subject_code IS NULL OR subject_code IN ('SENI-A1', 'BHS-A1', 'LOG-A1', 'MOT-A1', 'AGM-A1', 'SOS-A1')
) NOT VALID;
ALTER TABLE forum_posts VALIDATE CONSTRAINT forum_posts_subject_code_check;

CREATE INDEX idx_forum_threads_class_subject ON forum_posts(class_id, subject_code, created_at DESC, id DESC) WHERE parent_id IS NULL;
