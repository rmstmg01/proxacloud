DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cluster_secrets'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cluster_secrets DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cluster_secrets
        ADD CONSTRAINT cluster_secrets_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;

    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cluster_config_maps'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cluster_config_maps DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cluster_config_maps
        ADD CONSTRAINT cluster_config_maps_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;

    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cluster_image_pull_secrets'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cluster_image_pull_secrets DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cluster_image_pull_secrets
        ADD CONSTRAINT cluster_image_pull_secrets_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;

    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cluster_saved_manifests'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cluster_saved_manifests DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cluster_saved_manifests
        ADD CONSTRAINT cluster_saved_manifests_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;

    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cluster_saved_manifest_revisions'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cluster_saved_manifest_revisions DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cluster_saved_manifest_revisions
        ADD CONSTRAINT cluster_saved_manifest_revisions_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;

    SELECT conname INTO constraint_name
    FROM pg_constraint
    WHERE conrelid = 'cloudflare_tunnels'::regclass
      AND confrelid = 'clusters'::regclass
      AND contype = 'f'
    LIMIT 1;

    IF constraint_name IS NOT NULL THEN
        EXECUTE format('ALTER TABLE cloudflare_tunnels DROP CONSTRAINT %I', constraint_name);
    END IF;

    ALTER TABLE cloudflare_tunnels
        ALTER COLUMN cluster_id DROP NOT NULL;

    ALTER TABLE cloudflare_tunnels
        ADD CONSTRAINT cloudflare_tunnels_cluster_id_fkey
        FOREIGN KEY (cluster_id)
        REFERENCES clusters(id)
        ON DELETE SET NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_cloudflare_tunnels_global
    ON cloudflare_tunnels (namespace, deployment_name)
    WHERE cluster_id IS NULL;
