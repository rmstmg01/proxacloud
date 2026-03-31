CREATE TABLE IF NOT EXISTS metallb_configs (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL UNIQUE REFERENCES clusters(id) ON DELETE CASCADE,
    namespace TEXT NOT NULL DEFAULT 'metallb-system',
    version TEXT NOT NULL DEFAULT 'v0.14.9',
    address_pools JSONB NOT NULL DEFAULT '[]'::jsonb,
    interfaces JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL DEFAULT 'metallb-prepared',
    manifest_dir TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);