ALTER TABLE cluster_secrets
    ALTER COLUMN cluster_id DROP NOT NULL;

ALTER TABLE cluster_config_maps
    ALTER COLUMN cluster_id DROP NOT NULL;
