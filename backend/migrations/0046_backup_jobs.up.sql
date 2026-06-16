CREATE TABLE IF NOT EXISTS backup_jobs (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES backup_targets(id) ON DELETE RESTRICT,
    source_type TEXT NOT NULL,
    source_id BIGINT NOT NULL,
    source_name TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'snapshot',
    status TEXT NOT NULL DEFAULT 'pending',
    proxmox_node_name TEXT NOT NULL DEFAULT '',
    proxmox_task_id TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_jobs_target ON backup_jobs(target_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_source ON backup_jobs(source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_backup_jobs_created ON backup_jobs(created_at DESC);
