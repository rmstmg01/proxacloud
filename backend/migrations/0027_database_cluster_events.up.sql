CREATE TABLE IF NOT EXISTS database_cluster_events (
    id BIGSERIAL PRIMARY KEY,
    database_cluster_id BIGINT NOT NULL REFERENCES database_clusters(id) ON DELETE CASCADE,
    step TEXT NOT NULL,
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_database_cluster_events_cluster
    ON database_cluster_events (database_cluster_id, id ASC);
