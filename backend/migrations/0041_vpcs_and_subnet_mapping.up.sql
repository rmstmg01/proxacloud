CREATE TABLE IF NOT EXISTS vpcs (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    cidr TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO vpcs (name, cidr, description, is_default)
SELECT
    'default-vpc',
    COALESCE((SELECT cidr FROM network_profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1), '192.168.0.0/16'),
    'Default VPC created from existing network profiles',
    TRUE
WHERE NOT EXISTS (SELECT 1 FROM vpcs);

ALTER TABLE network_profiles
    ADD COLUMN IF NOT EXISTS vpc_id BIGINT REFERENCES vpcs(id) ON DELETE RESTRICT;

UPDATE network_profiles
SET vpc_id = (SELECT id FROM vpcs WHERE is_default = TRUE ORDER BY id ASC LIMIT 1)
WHERE vpc_id IS NULL;

ALTER TABLE network_profiles
    ALTER COLUMN vpc_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_network_profiles_vpc_id ON network_profiles(vpc_id);
