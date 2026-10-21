-- 000021 enrollments
-- A child is in a class through an enrollment, a record that says that the child asks to be, is, or was in that class, see
-- docs/adr/0014-class-enrollments.md. The class in students always names the class of the one active enrollment of the child, so every query
-- that reads students.class_id keeps working. This step only adds the table, keeps it in step with students and fills it for the children
-- that exist, and the routes that ask for a move and decide it come later.
CREATE TABLE enrollments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    student_id UUID NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    class_id UUID NOT NULL REFERENCES classes(id) ON DELETE RESTRICT,
    status VARCHAR(10) NOT NULL CHECK (status IN ('pending', 'active', 'ended', 'rejected')),
    requested_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    decided_at TIMESTAMP WITH TIME ZONE,
    decided_by_guru_id UUID REFERENCES gurus(id) ON DELETE SET NULL,
    activated_at TIMESTAMP WITH TIME ZONE,
    ended_at TIMESTAMP WITH TIME ZONE,
    -- what the child had in the class when the enrollment ended, so it can be listed as a previous class
    final_points INTEGER CHECK (final_points >= 0),
    final_season_points INTEGER CHECK (final_season_points >= 0),
    final_rank INTEGER CHECK (final_rank >= 0),
    final_scans INTEGER CHECK (final_scans >= 0),
    CONSTRAINT enrollments_activated_check CHECK ((status IN ('active', 'ended')) = (activated_at IS NOT NULL)),
    CONSTRAINT enrollments_ended_check CHECK (
        (status = 'ended') = (ended_at IS NOT NULL AND final_points IS NOT NULL AND final_season_points IS NOT NULL
            AND final_rank IS NOT NULL AND final_scans IS NOT NULL)
    ),
    CONSTRAINT enrollments_final_only_when_ended_check CHECK (
        status = 'ended' OR (ended_at IS NULL AND final_points IS NULL AND final_season_points IS NULL
            AND final_rank IS NULL AND final_scans IS NULL)
    )
);

-- a child has at most one active enrollment and at most one request that waits, and the first rule can be relaxed later if a child
-- may be in two classes
CREATE UNIQUE INDEX enrollments_active_child_key ON enrollments (student_id) WHERE status = 'active';
CREATE UNIQUE INDEX enrollments_pending_child_key ON enrollments (student_id) WHERE status = 'pending';
CREATE INDEX idx_enrollments_student ON enrollments (student_id, requested_at DESC);
CREATE INDEX idx_enrollments_class_status ON enrollments (class_id, status, requested_at);

-- A new child starts with an active enrollment in its class, whichever code path created it
CREATE FUNCTION enrollments_open_for_student() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO enrollments (student_id, class_id, status, requested_at, activated_at)
    VALUES (NEW.id, NEW.class_id, 'active', NEW.created_at, NEW.created_at);
    RETURN NEW;
END
$$;

CREATE TRIGGER students_open_enrollment AFTER INSERT ON students
    FOR EACH ROW EXECUTE FUNCTION enrollments_open_for_student();

-- A deleted child ends its active enrollment with the points, the rank and the scans it had, and a request that waits is dropped
CREATE FUNCTION enrollments_close_for_student() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE enrollments
    SET status = 'ended',
        ended_at = NEW.deleted_at,
        final_points = NEW.current_points,
        final_season_points = NEW.season_points,
        final_rank = NEW.rank_position,
        final_scans = (SELECT count(*) FROM trash_scans WHERE student_id = NEW.id)
    WHERE student_id = NEW.id AND status = 'active';

    DELETE FROM enrollments WHERE student_id = NEW.id AND status = 'pending';
    RETURN NEW;
END
$$;

CREATE TRIGGER students_close_enrollment AFTER UPDATE OF deleted_at ON students
    FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
    EXECUTE FUNCTION enrollments_close_for_student();

-- backfill
-- Every child that exists gets the enrollment it would have had, active for a child that is still there and ended for a deleted one.
-- A child that already has an enrollment is left alone, so the step can run again.
INSERT INTO enrollments (student_id, class_id, status, requested_at, activated_at)
SELECT s.id, s.class_id, 'active', s.created_at, s.created_at
FROM students s
WHERE s.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.student_id = s.id);

INSERT INTO enrollments (student_id, class_id, status, requested_at, activated_at, ended_at, final_points, final_season_points, final_rank, final_scans)
SELECT s.id, s.class_id, 'ended', s.created_at, s.created_at, s.deleted_at, s.current_points, s.season_points, s.rank_position,
    (SELECT count(*) FROM trash_scans t WHERE t.student_id = s.id)
FROM students s
WHERE s.deleted_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM enrollments e WHERE e.student_id = s.id);
