CREATE TABLE IF NOT EXISTS observability_configs (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL UNIQUE REFERENCES clusters(id) ON DELETE CASCADE,
    grafana_admin_user TEXT NOT NULL DEFAULT 'admin',
    grafana_admin_password TEXT NOT NULL,
    retention_days INTEGER NOT NULL DEFAULT 15,
    storage_class TEXT NOT NULL DEFAULT 'default',
    status TEXT NOT NULL DEFAULT 'prepared',
    dashboard_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS etcd_snapshots (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    snapshot_name TEXT NOT NULL,
    storage_target TEXT NOT NULL,
    snapshot_path TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS etcd_restore_runs (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    snapshot_id BIGINT NOT NULL REFERENCES etcd_snapshots(id) ON DELETE RESTRICT,
    status TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
