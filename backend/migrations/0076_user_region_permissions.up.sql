ALTER TABLE users ADD COLUMN IF NOT EXISTS region_access_mode TEXT NOT NULL DEFAULT 'all'
    CHECK (region_access_mode IN ('all', 'selected', 'none'));
-- Keep existing access, but require an explicit grant for newly created users.
ALTER TABLE users ALTER COLUMN region_access_mode SET DEFAULT 'none';

CREATE TABLE IF NOT EXISTS user_allowed_regions (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    region_id BIGINT NOT NULL REFERENCES regions(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, region_id)
);
