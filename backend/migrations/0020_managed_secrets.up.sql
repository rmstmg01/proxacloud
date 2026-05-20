CREATE TABLE IF NOT EXISTS managed_secrets (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL,
    name TEXT NOT NULL,
    nonce BYTEA NOT NULL,
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_managed_secrets_scope_name
    ON managed_secrets (scope, name);

ALTER TABLE proxmox_credentials
    ADD COLUMN IF NOT EXISTS password_secret_ref TEXT,
    ADD COLUMN IF NOT EXISTS api_token_secret_ref TEXT;

ALTER TABLE ssh_key_pairs
    ADD COLUMN IF NOT EXISTS private_key_secret_ref TEXT;

ALTER TABLE ssh_key_pairs
    ALTER COLUMN private_key DROP NOT NULL;

ALTER TABLE cluster_bootstrap_ssh_keys
    ADD COLUMN IF NOT EXISTS private_key_secret_ref TEXT;

ALTER TABLE cluster_bootstrap_ssh_keys
    ALTER COLUMN private_key DROP NOT NULL;

ALTER TABLE ssl_certificates
    ADD COLUMN IF NOT EXISTS private_key_secret_ref TEXT;

ALTER TABLE ssl_certificates
    ALTER COLUMN private_key_pem DROP NOT NULL;
