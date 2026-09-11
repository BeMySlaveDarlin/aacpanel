DO $$
BEGIN
    DELETE FROM hosts WHERE name = 'ATLAS';
    IF FOUND THEN
        RAISE NOTICE 'seeding hosts: the ATLAS row is deleted, there were no metrics for it';
    END IF;
EXCEPTION WHEN foreign_key_violation THEN
    RAISE NOTICE 'seeding hosts: the ATLAS row is used by metrics — it is a real host, keeping it';
END $$;
