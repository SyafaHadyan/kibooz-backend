DROP TRIGGER IF EXISTS students_close_enrollment ON students;
DROP TRIGGER IF EXISTS students_open_enrollment ON students;
DROP FUNCTION IF EXISTS enrollments_close_for_student();
DROP FUNCTION IF EXISTS enrollments_open_for_student();
DROP TABLE IF EXISTS enrollments;
