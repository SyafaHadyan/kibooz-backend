-- Validate forum subject check constraints in a separate transaction after 000034.
ALTER TABLE forum_posts VALIDATE CONSTRAINT forum_posts_subject_reply_check;
ALTER TABLE forum_posts VALIDATE CONSTRAINT forum_posts_subject_code_check;
