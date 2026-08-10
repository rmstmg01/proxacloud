CREATE TABLE IF NOT EXISTS managed_tunnel_domains (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    base_domain TEXT NOT NULL,
    cloudflare_zone_id TEXT NOT NULL,
    cloudflare_api_token_ref TEXT NOT NULL DEFAULT '',
    dns_target TEXT NOT NULL,
    dns_record_type TEXT NOT NULL DEFAULT 'CNAME',
    dns_proxied BOOLEAN NOT NULL DEFAULT TRUE,
    reserved_labels TEXT[] NOT NULL DEFAULT ARRAY['admin','api','mail','merchant','pos','www']::TEXT[],
    available_for_vps BOOLEAN NOT NULL DEFAULT TRUE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT managed_tunnel_domains_base_domain_check
        CHECK (base_domain = LOWER(base_domain) AND base_domain NOT LIKE '.%' AND base_domain NOT LIKE '%.'),
    CONSTRAINT managed_tunnel_domains_record_type_check
        CHECK (dns_record_type IN ('A', 'AAAA', 'CNAME'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_tunnel_domains_base_domain
    ON managed_tunnel_domains(LOWER(base_domain));

CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_tunnel_domains_default
    ON managed_tunnel_domains(is_default)
    WHERE is_default;

ALTER TABLE vps_tunnel_attachments
    ADD COLUMN IF NOT EXISTS managed_domain_id BIGINT NULL REFERENCES managed_tunnel_domains(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS hostname_label TEXT NOT NULL DEFAULT '';

ALTER TABLE vps_tunnel_http_routes
    ADD COLUMN IF NOT EXISTS managed_domain_id BIGINT NULL REFERENCES managed_tunnel_domains(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS idx_vps_tunnel_attachments_managed_domain
    ON vps_tunnel_attachments(managed_domain_id);

CREATE INDEX IF NOT EXISTS idx_vps_tunnel_http_routes_managed_domain
    ON vps_tunnel_http_routes(managed_domain_id);
