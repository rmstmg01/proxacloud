CREATE TABLE IF NOT EXISTS cluster_saved_manifests (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'default',
    description TEXT NOT NULL DEFAULT '',
    raw_manifest TEXT NOT NULL,
    is_global BOOLEAN NOT NULL DEFAULT FALSE,
    latest_revision_id BIGINT,
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cluster_saved_manifest_revisions (
    id BIGSERIAL PRIMARY KEY,
    manifest_id BIGINT NOT NULL REFERENCES cluster_saved_manifests(id) ON DELETE CASCADE,
    cluster_id BIGINT REFERENCES clusters(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL,
    raw_manifest TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (manifest_id, revision_number)
);

CREATE TABLE IF NOT EXISTS cluster_saved_manifest_apply_runs (
    id BIGSERIAL PRIMARY KEY,
    manifest_id BIGINT NOT NULL REFERENCES cluster_saved_manifests(id) ON DELETE CASCADE,
    revision_id BIGINT NOT NULL REFERENCES cluster_saved_manifest_revisions(id) ON DELETE CASCADE,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    kubectl_output TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'cluster_saved_manifests_latest_revision_fk'
          AND conrelid = 'cluster_saved_manifests'::regclass
    ) THEN
        ALTER TABLE cluster_saved_manifests
            ADD CONSTRAINT cluster_saved_manifests_latest_revision_fk
            FOREIGN KEY (latest_revision_id)
            REFERENCES cluster_saved_manifest_revisions(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_saved_manifests_unique_cluster
    ON cluster_saved_manifests (cluster_id, namespace, name)
    WHERE is_global = FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cluster_saved_manifests_unique_global
    ON cluster_saved_manifests (namespace, name)
    WHERE is_global = TRUE;

CREATE INDEX IF NOT EXISTS idx_cluster_saved_manifests_cluster_namespace
    ON cluster_saved_manifests (cluster_id, namespace, name);

CREATE INDEX IF NOT EXISTS idx_cluster_saved_manifest_revisions_manifest
    ON cluster_saved_manifest_revisions (manifest_id, revision_number DESC);

CREATE INDEX IF NOT EXISTS idx_cluster_saved_manifest_apply_runs_cluster
    ON cluster_saved_manifest_apply_runs (cluster_id, manifest_id, applied_at DESC);
