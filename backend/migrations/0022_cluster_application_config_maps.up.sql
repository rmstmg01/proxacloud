CREATE TABLE IF NOT EXISTS cluster_config_maps (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    namespace TEXT NOT NULL DEFAULT 'default',
    description TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL DEFAULT '{}',
    immutable BOOLEAN NOT NULL DEFAULT FALSE,
    labels JSONB NOT NULL DEFAULT '{}',
    annotations JSONB NOT NULL DEFAULT '{}',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (cluster_id, namespace, name)
);

CREATE TABLE IF NOT EXISTS application_manifest_config_maps (
    application_manifest_id BIGINT NOT NULL REFERENCES application_manifests(id) ON DELETE CASCADE,
    cluster_config_map_id BIGINT NOT NULL REFERENCES cluster_config_maps(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_manifest_id, cluster_config_map_id)
);

CREATE INDEX IF NOT EXISTS idx_cluster_config_maps_cluster_namespace
    ON cluster_config_maps (cluster_id, namespace, name);
