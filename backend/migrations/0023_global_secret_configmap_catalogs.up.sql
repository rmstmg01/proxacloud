ALTER TABLE cluster_secrets
    ADD COLUMN IF NOT EXISTS is_global BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE cluster_config_maps
    ADD COLUMN IF NOT EXISTS is_global BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_cluster_secrets_global
    ON cluster_secrets (is_global, namespace, name);

CREATE INDEX IF NOT EXISTS idx_cluster_config_maps_global
    ON cluster_config_maps (is_global, namespace, name);
