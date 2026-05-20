CREATE TABLE IF NOT EXISTS cluster_secrets (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'default',
    provider TEXT NOT NULL DEFAULT 'native',
    type TEXT NOT NULL DEFAULT 'Opaque',
    description TEXT NOT NULL DEFAULT '',
    external_ref TEXT NOT NULL DEFAULT '',
    provider_config JSONB NOT NULL DEFAULT '{}',
    immutable BOOLEAN NOT NULL DEFAULT FALSE,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, namespace, name)
);

CREATE TABLE IF NOT EXISTS cluster_secret_items (
    id BIGSERIAL PRIMARY KEY,
    cluster_secret_id BIGINT NOT NULL REFERENCES cluster_secrets(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    secret_ref TEXT,
    value_sha256 TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_secret_id, key)
);

CREATE TABLE IF NOT EXISTS application_manifest_secrets (
    application_manifest_id BIGINT NOT NULL REFERENCES application_manifests(id) ON DELETE CASCADE,
    cluster_secret_id BIGINT NOT NULL REFERENCES cluster_secrets(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_manifest_id, cluster_secret_id)
);

CREATE INDEX IF NOT EXISTS idx_cluster_secrets_cluster_namespace
    ON cluster_secrets (cluster_id, namespace, name);
