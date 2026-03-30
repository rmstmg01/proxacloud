CREATE TABLE IF NOT EXISTS cluster_bootstrap_configs (
    cluster_id BIGINT PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    ssh_user TEXT NOT NULL,
    ssh_private_key_path TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
