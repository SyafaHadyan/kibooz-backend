-- The time a post was last changed by its author, empty for a post that was never edited.
ALTER TABLE forum_posts ADD COLUMN edited_at TIMESTAMP WITH TIME ZONE;
