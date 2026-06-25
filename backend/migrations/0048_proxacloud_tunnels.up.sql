CREATE TABLE IF NOT EXISTS proxacloud_tunnels (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT REFERENCES clusters(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'proxacloud-agent',
    deployment_name TEXT NOT NULL DEFAULT 'proxacloud-agent',
    proxacloud_tunnel_id TEXT NOT NULL DEFAULT '',
    token_secret_ref TEXT NOT NULL DEFAULT '',
    gateway_url TEXT NOT NULL DEFAULT '',
    target_url TEXT NOT NULL DEFAULT '',
    replicas INTEGER NOT NULL DEFAULT 1,
    image TEXT NOT NULL DEFAULT '',
    image_pull_secret TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'configured',
    last_manifest TEXT NOT NULL DEFAULT '',
    last_apply_output TEXT NOT NULL DEFAULT '',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS proxacloud_tunnel_routes (
    id BIGSERIAL PRIMARY KEY,
    tunnel_id BIGINT NOT NULL REFERENCES proxacloud_tunnels(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    service_namespace TEXT NOT NULL DEFAULT '',
    service_name TEXT NOT NULL DEFAULT '',
    service_port INTEGER NOT NULL DEFAULT 80,
    service_scheme TEXT NOT NULL DEFAULT 'http',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tunnel_id, hostname, path)
);

CREATE TABLE IF NOT EXISTS proxacloud_tunnel_events (
    id BIGSERIAL PRIMARY KEY,
    tunnel_id BIGINT NOT NULL REFERENCES proxacloud_tunnels(id) ON DELETE CASCADE,
    step TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_proxacloud_tunnels_cluster
    ON proxacloud_tunnels (cluster_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_proxacloud_tunnel_routes_tunnel
    ON proxacloud_tunnel_routes (tunnel_id, hostname ASC);

CREATE INDEX IF NOT EXISTS idx_proxacloud_tunnel_events_tunnel
    ON proxacloud_tunnel_events (tunnel_id, id ASC);
