CREATE TABLE IF NOT EXISTS proxmox_node_network_tags (
    proxmox_node_name TEXT PRIMARY KEY,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
