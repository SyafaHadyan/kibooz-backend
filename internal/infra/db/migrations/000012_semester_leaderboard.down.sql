DROP TABLE IF EXISTS leaderboard_history;
DROP TABLE IF EXISTS leaderboard_seasons;

DROP INDEX IF EXISTS idx_students_class_season_points;
CREATE INDEX idx_students_class_points ON students(class_id, current_points DESC, full_name, id);

ALTER TABLE students DROP COLUMN IF EXISTS season_points;
