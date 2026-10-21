-- 000022 enrollment requests
-- A request to move waits for a teacher of the new class, so a class that is archived or that closes joining answers every request that
-- waits with a rejection, see docs/adr/0014-class-enrollments.md. A trigger does it so that no code path that changes a class can forget it.
CREATE FUNCTION enrollments_reject_for_class() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE enrollments SET status = 'rejected', decided_at = now() WHERE class_id = NEW.id AND status = 'pending';
    RETURN NEW;
END
$$;

CREATE TRIGGER classes_reject_requests AFTER UPDATE OF join_open, archived_at ON classes
    FOR EACH ROW WHEN ((OLD.join_open AND NOT NEW.join_open) OR (OLD.archived_at IS NULL AND NEW.archived_at IS NOT NULL))
    EXECUTE FUNCTION enrollments_reject_for_class();

-- The scans that a child saves on an enrollment that ends are the ones of that class only, so a child that moved before counts the scans
-- since it joined the class that it leaves
CREATE OR REPLACE FUNCTION enrollments_close_for_student() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE enrollments AS e
    SET status = 'ended',
        ended_at = NEW.deleted_at,
        final_points = NEW.current_points,
        final_season_points = NEW.season_points,
        final_rank = NEW.rank_position,
        final_scans = (SELECT count(*) FROM trash_scans WHERE student_id = NEW.id AND scanned_at >= e.activated_at)
    WHERE e.student_id = NEW.id AND e.status = 'active';

    DELETE FROM enrollments WHERE student_id = NEW.id AND status = 'pending';
    RETURN NEW;
END
$$;
