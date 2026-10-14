-- The leaderboard ranks the points of the current semester, and the points of a child over all semesters stay in
-- current_points. A semester starts from zero, and the final standings of the one that ended are kept in
-- leaderboard_history. leaderboard_seasons holds one row for every semester that has begun, so the reset runs once per semester
-- whichever instance of the API notices first.

ALTER TABLE students ADD COLUMN season_points INTEGER NOT NULL DEFAULT 0 CHECK (season_points >= 0);

-- the semester that is under way continues with the points it already has
UPDATE students SET season_points = current_points;

DROP INDEX idx_students_class_points;
CREATE INDEX idx_students_class_season_points ON students(class_id, season_points DESC, full_name, id);

CREATE TABLE leaderboard_seasons (
    label TEXT PRIMARY KEY,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE TABLE leaderboard_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    class_id UUID NOT NULL REFERENCES classes(id),
    student_id UUID NOT NULL,
    student_name VARCHAR(150) NOT NULL,
    season_label TEXT NOT NULL,
    points INTEGER NOT NULL,
    rank_position INTEGER NOT NULL,
    recorded_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

CREATE INDEX idx_leaderboard_history_class ON leaderboard_history(class_id, season_label, rank_position);
