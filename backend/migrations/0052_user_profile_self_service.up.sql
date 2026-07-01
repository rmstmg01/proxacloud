ALTER TABLE users
    ADD COLUMN IF NOT EXISTS first_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_name TEXT NOT NULL DEFAULT '';

UPDATE users
SET
    first_name = CASE
        WHEN first_name = '' AND btrim(full_name) <> '' THEN split_part(btrim(full_name), ' ', 1)
        ELSE first_name
    END,
    last_name = CASE
        WHEN last_name = '' AND btrim(full_name) LIKE '% %' THEN COALESCE(NULLIF(regexp_replace(btrim(full_name), '^\S+\s*', ''), ''), '')
        ELSE last_name
    END
WHERE btrim(full_name) <> '';
