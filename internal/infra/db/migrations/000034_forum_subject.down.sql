ALTER TABLE forum_posts DROP CONSTRAINT IF EXISTS forum_posts_subject_code_check;
ALTER TABLE forum_posts DROP CONSTRAINT IF EXISTS forum_posts_subject_reply_check;
ALTER TABLE forum_posts DROP COLUMN IF EXISTS subject_code;
