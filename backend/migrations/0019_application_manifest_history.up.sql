CREATE TABLE IF NOT EXISTS application_manifests (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL,
    hostname TEXT NOT NULL DEFAULT '',
    ingress_class TEXT NOT NULL DEFAULT '',
    tls_secret_name TEXT NOT NULL DEFAULT '',
    tls_certificate_id BIGINT REFERENCES ssl_certificates(id) ON DELETE SET NULL,
    latest_revision_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, namespace, name)
);

CREATE TABLE IF NOT EXISTS application_manifest_revisions (
    id BIGSERIAL PRIMARY KEY,
    application_manifest_id BIGINT NOT NULL REFERENCES application_manifests(id) ON DELETE CASCADE,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    revision_number INTEGER NOT NULL,
    raw_manifest TEXT NOT NULL,
    rendered_manifest TEXT NOT NULL,
    kubectl_output TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    apply BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (application_manifest_id, revision_number)
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'application_manifests_latest_revision_fk'
          AND conrelid = 'application_manifests'::regclass
    ) THEN
        ALTER TABLE application_manifests
            ADD CONSTRAINT application_manifests_latest_revision_fk
            FOREIGN KEY (latest_revision_id)
            REFERENCES application_manifest_revisions(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_application_manifests_cluster
    ON application_manifests(cluster_id, namespace, name);

CREATE INDEX IF NOT EXISTS idx_application_manifest_revisions_app
    ON application_manifest_revisions(application_manifest_id, revision_number DESC);
