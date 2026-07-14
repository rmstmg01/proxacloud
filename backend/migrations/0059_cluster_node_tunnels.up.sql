ALTER TABLE cluster_nodes
    ADD COLUMN IF NOT EXISTS hostname TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS fqdn TEXT NOT NULL DEFAULT '';

UPDATE cluster_nodes SET hostname = vm_name WHERE hostname = '';

CREATE TABLE IF NOT EXISTS cluster_node_tunnel_attachments (
    id BIGSERIAL PRIMARY KEY,
    cluster_node_id BIGINT NOT NULL REFERENCES cluster_nodes(id) ON DELETE CASCADE,
    tunnel_id TEXT NOT NULL DEFAULT '',
    http_route_id TEXT NOT NULL DEFAULT '',
    tcp_route_id TEXT NOT NULL DEFAULT '',
    token_id TEXT NOT NULL DEFAULT '',
    public_hostname TEXT NOT NULL,
    public_port INTEGER NOT NULL,
    cloudflare_dns_record_id TEXT NOT NULL DEFAULT '',
    cloudflare_dns_record_type TEXT NOT NULL DEFAULT '',
    cloudflare_dns_record_content TEXT NOT NULL DEFAULT '',
    cloudflare_dns_proxied BOOLEAN NOT NULL DEFAULT FALSE,
    cloudflare_dns_status TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_node_id),
    UNIQUE (public_hostname, public_port)
);

CREATE INDEX IF NOT EXISTS idx_cluster_node_tunnel_status ON cluster_node_tunnel_attachments(status);
