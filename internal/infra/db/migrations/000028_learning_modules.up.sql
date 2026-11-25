-- Learning modules track scheduled class meetings created by teachers and viewed by teachers and parents.

CREATE TABLE learning_modules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    class_id UUID NOT NULL REFERENCES classes(id) ON DELETE CASCADE,
    added_by_guru_id UUID NOT NULL REFERENCES gurus(id) ON DELETE RESTRICT,
    meeting_number INTEGER NOT NULL CHECK (meeting_number >= 1 AND meeting_number <= 100),
    title VARCHAR(150) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'upcoming' CHECK (status IN ('completed', 'ongoing', 'upcoming')),
    scheduled_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_learning_modules_class ON learning_modules(class_id, meeting_number, created_at, id);
