CREATE TABLE IF NOT EXISTS regions (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS region_nodes (
    region_id BIGINT NOT NULL REFERENCES regions(id) ON DELETE CASCADE,
    proxmox_node_id BIGINT NOT NULL REFERENCES proxmox_nodes(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (region_id, proxmox_node_id)
);

CREATE TABLE IF NOT EXISTS region_networks (
    region_id BIGINT NOT NULL REFERENCES regions(id) ON DELETE CASCADE,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (region_id, network_profile_id)
);

ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS region_id BIGINT REFERENCES regions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_region_nodes_region_id ON region_nodes(region_id);
CREATE INDEX IF NOT EXISTS idx_region_networks_region_id ON region_networks(region_id);
CREATE INDEX IF NOT EXISTS idx_vps_instances_region_id ON vps_instances(region_id);

INSERT INTO regions (name, slug, description, enabled, is_default)
SELECT 'Default Location', 'default-location', 'Default location for existing Proxmox nodes and networks.', TRUE, TRUE
WHERE NOT EXISTS (SELECT 1 FROM regions);

INSERT INTO region_nodes (region_id, proxmox_node_id)
SELECT r.id, n.id
FROM regions r
CROSS JOIN proxmox_nodes n
WHERE r.is_default = TRUE
  AND n.enabled = TRUE
ON CONFLICT DO NOTHING;

INSERT INTO region_networks (region_id, network_profile_id)
SELECT r.id, p.id
FROM regions r
CROSS JOIN network_profiles p
WHERE r.is_default = TRUE
ON CONFLICT DO NOTHING;
