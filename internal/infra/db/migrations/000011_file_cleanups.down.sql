-- The rows that pointed at these files are already cleared, so the queue holds the only record of them. Dropping it with
-- files waiting would leave them in the bucket for good, so the downgrade stops until the API has swept the queue.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM file_cleanups) THEN
        RAISE EXCEPTION 'file_cleanups still holds files that are waiting to be deleted from the bucket';
    END IF;
END $$;

DROP TABLE IF EXISTS file_cleanups;
