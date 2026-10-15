-- One teacher owns each class. The owner can remove other teachers and hand the class to another teacher, and every
-- teacher can leave. The teacher who joined first among the active ones becomes the owner of the classes that exist now.

ALTER TABLE class_teachers ADD COLUMN is_owner BOOLEAN NOT NULL DEFAULT false;

UPDATE class_teachers AS ct
SET is_owner = true
FROM (
    SELECT DISTINCT ON (t.class_id) t.class_id, t.guru_id
    FROM class_teachers AS t
    JOIN gurus AS g ON g.id = t.guru_id AND g.deleted_at IS NULL
    ORDER BY t.class_id, t.created_at, t.guru_id
) AS first
WHERE ct.class_id = first.class_id AND ct.guru_id = first.guru_id;

-- at most one owner for a class, so two changes that overlap can never leave a class with two
CREATE UNIQUE INDEX idx_class_teachers_owner ON class_teachers(class_id) WHERE is_owner;
