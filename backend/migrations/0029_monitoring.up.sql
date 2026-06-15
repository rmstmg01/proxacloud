CREATE TABLE IF NOT EXISTS monitoring_stacks (
    id BIGSERIAL PRIMARY KEY,
    namespace TEXT NOT NULL DEFAULT 'monitoring',
    grafana_domain TEXT NOT NULL DEFAULT 'grafana.proxacloud.com',
    storage_class TEXT NOT NULL DEFAULT '',
    prometheus_storage_size TEXT NOT NULL DEFAULT '50Gi',
    grafana_storage_size TEXT NOT NULL DEFAULT '10Gi',
    retention TEXT NOT NULL DEFAULT '15d',
    expose_grafana BOOLEAN NOT NULL DEFAULT TRUE,
    expose_prometheus BOOLEAN NOT NULL DEFAULT FALSE,
    expose_alertmanager BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'planned',
    last_values_yaml TEXT NOT NULL DEFAULT '',
    last_manifest TEXT NOT NULL DEFAULT '',
    last_apply_output TEXT NOT NULL DEFAULT '',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS monitored_clusters (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT REFERENCES clusters(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT 'production',
    mode TEXT NOT NULL DEFAULT 'in_cluster',
    status TEXT NOT NULL DEFAULT 'configured',
    prometheus_url TEXT NOT NULL DEFAULT '',
    grafana_url TEXT NOT NULL DEFAULT '',
    agent_namespace TEXT NOT NULL DEFAULT 'monitoring',
    remote_write_url TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    last_manifest TEXT NOT NULL DEFAULT '',
    last_apply_output TEXT NOT NULL DEFAULT '',
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS idx_monitored_clusters_cluster
    ON monitored_clusters (cluster_id);

CREATE INDEX IF NOT EXISTS idx_monitored_clusters_status
    ON monitored_clusters (status, environment);
