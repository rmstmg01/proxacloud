CREATE TABLE IF NOT EXISTS vps_tunnel_attachments (
    id BIGSERIAL PRIMARY KEY,
    vps_id BIGINT NOT NULL REFERENCES vps_instances(id) ON DELETE CASCADE,
    customer_account_id BIGINT REFERENCES customer_accounts(id) ON DELETE SET NULL,
    tunnel_id TEXT NOT NULL DEFAULT '',
    http_route_id TEXT NOT NULL DEFAULT '',
    tcp_route_id TEXT NOT NULL DEFAULT '',
    token_id TEXT NOT NULL DEFAULT '',
    public_hostname TEXT NOT NULL,
    public_port INTEGER NOT NULL,
    http_target_url TEXT NOT NULL DEFAULT 'http://127.0.0.1:80',
    tcp_target_host TEXT NOT NULL DEFAULT '127.0.0.1',
    tcp_target_port INTEGER NOT NULL DEFAULT 22,
    ssh_user TEXT NOT NULL DEFAULT '',
    install_command_redacted TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    emailed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (vps_id),
    UNIQUE (public_hostname, public_port)
);

CREATE INDEX IF NOT EXISTS idx_vps_tunnel_attachments_customer_account_id
    ON vps_tunnel_attachments(customer_account_id);

CREATE INDEX IF NOT EXISTS idx_vps_tunnel_attachments_status
    ON vps_tunnel_attachments(status);
