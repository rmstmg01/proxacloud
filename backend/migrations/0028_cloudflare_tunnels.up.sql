CREATE TABLE IF NOT EXISTS cloudflare_tunnels (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'cloudflare-tunnel',
    deployment_name TEXT NOT NULL DEFAULT 'cloudflared',
    routing_mode TEXT NOT NULL DEFAULT 'traefik',
    provisioning_mode TEXT NOT NULL DEFAULT 'manual_token',
    cloudflare_account_id TEXT NOT NULL DEFAULT '',
    cloudflare_zone_id TEXT NOT NULL DEFAULT '',
    cloudflare_tunnel_id TEXT NOT NULL DEFAULT '',
    cloudflare_tunnel_name TEXT NOT NULL DEFAULT '',
    token_secret_ref TEXT NOT NULL DEFAULT '',
    api_token_secret_ref TEXT NOT NULL DEFAULT '',
    replicas INTEGER NOT NULL DEFAULT 3,
    image TEXT NOT NULL DEFAULT 'cloudflare/cloudflared:latest',
    ingress_namespace TEXT NOT NULL DEFAULT 'traefik',
    ingress_service_name TEXT NOT NULL DEFAULT 'traefik',
    ingress_service_port INTEGER NOT NULL DEFAULT 80,
    ingress_service_scheme TEXT NOT NULL DEFAULT 'http',
    configure_dns BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'configured',
    last_manifest TEXT NOT NULL DEFAULT '',
    last_apply_output TEXT NOT NULL DEFAULT '',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, namespace, deployment_name)
);

CREATE TABLE IF NOT EXISTS cloudflare_tunnel_routes (
    id BIGSERIAL PRIMARY KEY,
    tunnel_id BIGINT NOT NULL REFERENCES cloudflare_tunnels(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    service_namespace TEXT NOT NULL DEFAULT 'default',
    service_name TEXT NOT NULL,
    service_port INTEGER NOT NULL DEFAULT 80,
    service_scheme TEXT NOT NULL DEFAULT 'http',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tunnel_id, hostname, path)
);

CREATE TABLE IF NOT EXISTS cloudflare_tunnel_events (
    id BIGSERIAL PRIMARY KEY,
    tunnel_id BIGINT NOT NULL REFERENCES cloudflare_tunnels(id) ON DELETE CASCADE,
    step TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cloudflare_tunnels_cluster
    ON cloudflare_tunnels (cluster_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cloudflare_tunnel_routes_tunnel
    ON cloudflare_tunnel_routes (tunnel_id, hostname ASC);

CREATE INDEX IF NOT EXISTS idx_cloudflare_tunnel_events_tunnel
    ON cloudflare_tunnel_events (tunnel_id, id ASC);
