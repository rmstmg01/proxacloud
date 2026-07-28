CREATE TABLE IF NOT EXISTS managed_storefront_endpoints (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL DEFAULT 'bikri',
    external_id TEXT NOT NULL,
    hostname TEXT NOT NULL,
    tunnel_id TEXT NOT NULL,
    target_url TEXT NOT NULL,
    http_route_id TEXT NOT NULL DEFAULT '',
    dns_record_id TEXT NOT NULL DEFAULT '',
    dns_record_type TEXT NOT NULL DEFAULT '',
    dns_record_content TEXT NOT NULL DEFAULT '',
    dns_proxied BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, external_id),
    UNIQUE (hostname),
    CONSTRAINT managed_storefront_endpoints_status_check
        CHECK (status IN ('pending', 'active', 'failed', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_storefront_endpoints_route
    ON managed_storefront_endpoints(http_route_id)
    WHERE http_route_id <> '';
