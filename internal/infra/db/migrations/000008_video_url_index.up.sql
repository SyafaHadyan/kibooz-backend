-- CountVideosByURL looks videos up by address on every staged add and every delete of an uploaded file, while a lock is held.
-- Only equality is needed, and a hash index has no limit on the length of an address, unlike a btree.
CREATE INDEX idx_learning_videos_url ON learning_videos USING HASH (video_url);
