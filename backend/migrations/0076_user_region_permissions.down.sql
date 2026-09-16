DROP TABLE IF EXISTS user_allowed_regions;
ALTER TABLE users DROP COLUMN IF EXISTS region_access_mode;
