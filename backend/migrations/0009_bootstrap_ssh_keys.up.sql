CREATE TABLE IF NOT EXISTS cluster_bootstrap_ssh_keys (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    key_name TEXT NOT NULL,
    private_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, key_name)
);

ALTER TABLE cluster_bootstrap_configs
    ADD COLUMN IF NOT EXISTS last_ssh_key_id BIGINT REFERENCES cluster_bootstrap_ssh_keys(id);
