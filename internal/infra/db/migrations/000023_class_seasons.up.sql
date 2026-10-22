-- 000023 class seasons
-- The reset of the leaderboard at a new semester took the ranking lock of every class in one transaction. That holds one lock for
-- each class until the end, which a PostgreSQL server only has room for until a few thousand classes, and every scan waits for
-- the whole run. Each class now keeps the label of the semester that its points belong to, so a reset can handle one class
-- in a short transaction of its own, any copy of the API can continue where another stopped, and a class is never reset twice.
-- The label is kept in its own table and not on classes, so the reset never waits for a change of the join code or of the archive.
-- A deploy during the first minute of a semester, while a copy that still runs the old reset is up, resets the classes twice, so avoid it.
CREATE TABLE class_seasons (
    class_id UUID PRIMARY KEY REFERENCES classes(id) ON DELETE CASCADE,
    season_label TEXT
);

-- every class holds the points of the semester that is under way, and the label stays empty on a database that has none yet
INSERT INTO class_seasons (class_id, season_label)
SELECT c.id, (SELECT label FROM leaderboard_seasons ORDER BY started_at DESC, label DESC LIMIT 1)
FROM classes AS c;

-- a class that is created later belongs to the semester that is under way, so a reset that is half done never touches it
CREATE FUNCTION class_seasons_open() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO class_seasons (class_id, season_label)
    VALUES (NEW.id, (SELECT label FROM leaderboard_seasons ORDER BY started_at DESC, label DESC LIMIT 1));
    RETURN NEW;
END
$$;

CREATE TRIGGER classes_open_season AFTER INSERT ON classes
    FOR EACH ROW EXECUTE FUNCTION class_seasons_open();
