DROP TRIGGER IF EXISTS classes_reject_requests ON classes;
DROP FUNCTION IF EXISTS enrollments_reject_for_class();

CREATE OR REPLACE FUNCTION enrollments_close_for_student() RETURNS trigger LANGUAGE plpgsql AS $$
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
