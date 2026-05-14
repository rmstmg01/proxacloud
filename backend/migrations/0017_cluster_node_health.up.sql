-- Cluster node health tracking for automatic recovery
CREATE TABLE IF NOT EXISTS cluster_node_health (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    cluster_node_id BIGINT NOT NULL REFERENCES cluster_nodes(id) ON DELETE CASCADE,
    vm_name TEXT NOT NULL,
    role TEXT NOT NULL,
    health_status TEXT NOT NULL DEFAULT 'unknown', -- 'healthy', 'unhealthy', 'missing', 'unknown'
    last_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(cluster_id, cluster_node_id)
);

CREATE INDEX IF NOT EXISTS idx_cluster_node_health_cluster_status
    ON cluster_node_health(cluster_id, health_status);

CREATE INDEX IF NOT EXISTS idx_cluster_node_health_failures
    ON cluster_node_health(cluster_id, consecutive_failures DESC);

-- Auto-recovery configuration per cluster
CREATE TABLE IF NOT EXISTS cluster_recovery_config (
    cluster_id BIGINT PRIMARY KEY REFERENCES clusters(id) ON DELETE CASCADE,
    auto_recovery_enabled BOOLEAN NOT NULL DEFAULT false,
    health_check_interval_seconds INTEGER NOT NULL DEFAULT 30,
    failure_threshold_count INTEGER NOT NULL DEFAULT 3,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Recovery event log for audit trail
CREATE TABLE IF NOT EXISTS cluster_recovery_events (
    id BIGSERIAL PRIMARY KEY,
    cluster_id BIGINT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    cluster_node_id BIGINT REFERENCES cluster_nodes(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL, -- 'health_check_started', 'health_check_failed', 'recovery_triggered', 'recovery_completed', 'recovery_failed'
    vm_name TEXT,
    role TEXT,
    status TEXT NOT NULL,
    message TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cluster_recovery_events_cluster
    ON cluster_recovery_events(cluster_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cluster_recovery_events_node
    ON cluster_recovery_events(cluster_node_id, created_at DESC);
