DROP INDEX IF EXISTS idx_vps_tunnel_http_routes_managed_domain;
DROP INDEX IF EXISTS idx_vps_tunnel_attachments_managed_domain;

ALTER TABLE vps_tunnel_http_routes
    DROP COLUMN IF EXISTS managed_domain_id;

ALTER TABLE vps_tunnel_attachments
    DROP COLUMN IF EXISTS hostname_label,
    DROP COLUMN IF EXISTS managed_domain_id;

DROP INDEX IF EXISTS idx_managed_tunnel_domains_default;
DROP INDEX IF EXISTS idx_managed_tunnel_domains_base_domain;
DROP TABLE IF EXISTS managed_tunnel_domains;
