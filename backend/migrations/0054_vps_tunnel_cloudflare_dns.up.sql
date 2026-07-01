ALTER TABLE vps_tunnel_attachments
    ADD COLUMN IF NOT EXISTS cloudflare_dns_record_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cloudflare_dns_record_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cloudflare_dns_record_content TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cloudflare_dns_proxied BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS cloudflare_dns_status TEXT NOT NULL DEFAULT '';
