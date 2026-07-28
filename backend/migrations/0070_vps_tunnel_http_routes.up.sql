CREATE TABLE IF NOT EXISTS vps_tunnel_http_routes (
    id BIGSERIAL PRIMARY KEY,
    tunnel_attachment_id BIGINT NOT NULL REFERENCES vps_tunnel_attachments(id) ON DELETE CASCADE,
    http_route_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    public_hostname TEXT NOT NULL,
    target_url TEXT NOT NULL,
    tls_mode TEXT NOT NULL DEFAULT 'edge',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    cloudflare_dns_record_id TEXT NOT NULL DEFAULT '',
    cloudflare_dns_record_type TEXT NOT NULL DEFAULT '',
    cloudflare_dns_record_content TEXT NOT NULL DEFAULT '',
    cloudflare_dns_proxied BOOLEAN NOT NULL DEFAULT FALSE,
    cloudflare_dns_status TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (public_hostname),
    CONSTRAINT vps_tunnel_http_routes_tls_mode_check
        CHECK (tls_mode IN ('edge', 'passthrough', 'reencrypt'))
);

CREATE INDEX IF NOT EXISTS idx_vps_tunnel_http_routes_attachment
    ON vps_tunnel_http_routes(tunnel_attachment_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS idx_vps_tunnel_http_routes_remote_id
    ON vps_tunnel_http_routes(http_route_id)
    WHERE http_route_id <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_vps_tunnel_http_routes_primary
    ON vps_tunnel_http_routes(tunnel_attachment_id)
    WHERE is_primary = TRUE;

INSERT INTO vps_tunnel_http_routes (
    tunnel_attachment_id,
    http_route_id,
    name,
    public_hostname,
    target_url,
    tls_mode,
    enabled,
    is_primary,
    cloudflare_dns_record_id,
    cloudflare_dns_record_type,
    cloudflare_dns_record_content,
    cloudflare_dns_proxied,
    cloudflare_dns_status,
    status,
    last_error,
    created_at,
    updated_at
)
SELECT
    id,
    http_route_id,
    'Primary web route',
    public_hostname,
    http_target_url,
    'edge',
    TRUE,
    TRUE,
    cloudflare_dns_record_id,
    cloudflare_dns_record_type,
    cloudflare_dns_record_content,
    cloudflare_dns_proxied,
    cloudflare_dns_status,
    CASE
        WHEN http_route_id <> '' THEN 'active'
        WHEN status = 'failed' THEN 'failed'
        ELSE 'pending'
    END,
    last_error,
    created_at,
    updated_at
FROM vps_tunnel_attachments
ON CONFLICT (public_hostname) DO NOTHING;
