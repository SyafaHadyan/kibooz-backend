-- 000024 leaderboard history unique
-- A class saves the standings of a semester once, one row for each child. Nothing in the table said so, which let a second save of
-- the same semester store the rows again without any error. The earliest row of each key is the one that was saved when the
-- semester ended, so it stays and any later copy goes before the key is added.
-- The table is locked against writes first, so a save that commits between the delete and the index cannot make the index fail
-- and leave the database marked dirty. Reads go on, and the table is small.
LOCK TABLE leaderboard_history IN SHARE ROW EXCLUSIVE MODE;

DELETE FROM leaderboard_history AS later
USING leaderboard_history AS earlier
WHERE later.class_id = earlier.class_id
  AND later.student_id = earlier.student_id
  AND later.season_label = earlier.season_label
  AND (later.recorded_at, later.id) > (earlier.recorded_at, earlier.id);

-- the child comes first, because the rename of a child updates its rows by student_id and nothing else indexes that column
CREATE UNIQUE INDEX leaderboard_history_unique ON leaderboard_history (student_id, class_id, season_label);
