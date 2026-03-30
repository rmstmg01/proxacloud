CREATE TABLE IF NOT EXISTS proxmox_credentials (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT NOT NULL,
    username TEXT,
    password TEXT,
    api_token_id TEXT,
    api_token_secret TEXT,
    insecure_tls BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proxmox_nodes (
    id BIGSERIAL PRIMARY KEY,
    credential_id BIGINT NOT NULL REFERENCES proxmox_credentials(id) ON DELETE CASCADE,
    node_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    cpu_count INTEGER NOT NULL DEFAULT 0,
    memory_total_bytes BIGINT NOT NULL DEFAULT 0,
    memory_used_bytes BIGINT NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    last_discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (credential_id, node_name)
);
