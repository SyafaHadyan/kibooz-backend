-- Join codes now have eight characters, which makes guessing one far harder. A class that still has a shorter code gets a
-- new one, and the old code stops working. The characters come from the random bytes of a generated UUID, and not from
-- random(), so that a code cannot be predicted. 256 is a multiple of 32, so the remainder of a byte picks each character of
-- the alphabet with the same chance. The bytes 6 and 8 of a UUID hold a version and a variant, so they are not used.
DO $$
DECLARE
    alphabet CONSTANT text := 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
    short_class RECORD;
    random_bytes bytea;
    candidate text;
    idx int;
BEGIN
    FOR short_class IN SELECT id FROM classes WHERE length(join_code) < 8 LOOP
        LOOP
            random_bytes := uuid_send(gen_random_uuid());
            candidate := '';

            FOREACH idx IN ARRAY ARRAY[0, 1, 2, 3, 4, 5, 9, 10] LOOP
                candidate := candidate || substr(alphabet, 1 + (get_byte(random_bytes, idx) % 32), 1);
            END LOOP;

            EXIT WHEN NOT EXISTS (SELECT 1 FROM classes WHERE join_code = candidate);
        END LOOP;

        UPDATE classes SET join_code = candidate WHERE id = short_class.id;
    END LOOP;
END
$$;
