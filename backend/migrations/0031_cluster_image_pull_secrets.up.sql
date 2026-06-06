CREATE TABLE IF NOT EXISTS cluster_image_pull_secrets (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'default',
    registry_server TEXT NOT NULL DEFAULT 'ghcr.io',
    username TEXT NOT NULL,
    password_ref TEXT NOT NULL DEFAULT '',
    password_sha256 TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    is_global BOOLEAN NOT NULL DEFAULT FALSE,
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, namespace, name)
);

CREATE TABLE IF NOT EXISTS application_manifest_image_pull_secrets (
    application_manifest_id BIGINT NOT NULL REFERENCES application_manifests(id) ON DELETE CASCADE,
    cluster_image_pull_secret_id BIGINT NOT NULL REFERENCES cluster_image_pull_secrets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_manifest_id, cluster_image_pull_secret_id)
);

CREATE INDEX IF NOT EXISTS idx_cluster_image_pull_secrets_cluster_namespace
    ON cluster_image_pull_secrets (cluster_id, namespace, name);

CREATE INDEX IF NOT EXISTS idx_cluster_image_pull_secrets_global
    ON cluster_image_pull_secrets (is_global, namespace, name);
