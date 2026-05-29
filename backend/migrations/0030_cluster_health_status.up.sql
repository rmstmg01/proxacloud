ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS health_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS health_checked_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_clusters_health_status
    ON clusters (health_status);
