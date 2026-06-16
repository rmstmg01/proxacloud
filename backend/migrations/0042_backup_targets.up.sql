CREATE TABLE IF NOT EXISTS backup_targets (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    access_key_secret_ref TEXT NOT NULL DEFAULT '',
    secret_key_secret_ref TEXT NOT NULL DEFAULT '',
    password_secret_ref TEXT NOT NULL DEFAULT '',
    ssh_private_key_secret_ref TEXT NOT NULL DEFAULT '',
    restic_password_secret_ref TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_targets_type ON backup_targets(type);
CREATE INDEX IF NOT EXISTS idx_backup_targets_enabled ON backup_targets(enabled);
