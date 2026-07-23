INSERT INTO billing_prices (
    resource_type, display_name, base_monthly_cents, cpu_monthly_cents,
    memory_gb_monthly_cents, disk_gb_monthly_cents, enabled
)
VALUES
    ('vps_backup_monthly', 'Monthly VPS Backup', 200, 0, 0, 1, TRUE),
    ('vps_backup_weekly', 'Weekly VPS Backup', 500, 0, 0, 2, TRUE),
    ('vps_backup_daily', 'Daily VPS Backup', 1000, 0, 0, 4, TRUE)
ON CONFLICT (resource_type) DO NOTHING;

CREATE TABLE IF NOT EXISTS vps_backup_policies (
    id BIGSERIAL PRIMARY KEY,
    vps_id BIGINT NOT NULL UNIQUE REFERENCES vps_instances(id) ON DELETE CASCADE,
    customer_account_id BIGINT REFERENCES customer_accounts(id) ON DELETE CASCADE,
    target_id BIGINT NOT NULL REFERENCES backup_targets(id) ON DELETE RESTRICT,
    plan_code TEXT NOT NULL,
    frequency TEXT NOT NULL,
    weekday SMALLINT,
    month_day SMALLINT,
    local_time TIME NOT NULL,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    mode TEXT NOT NULL DEFAULT 'snapshot',
    retention_count INTEGER NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    CHECK (mode IN ('snapshot', 'suspend', 'stop')),
    CHECK (weekday IS NULL OR weekday BETWEEN 0 AND 6),
    CHECK (month_day IS NULL OR month_day BETWEEN 1 AND 28),
    CHECK (retention_count > 0)
);

CREATE INDEX IF NOT EXISTS idx_vps_backup_policies_due
    ON vps_backup_policies (next_run_at) WHERE enabled = TRUE;

ALTER TABLE backup_jobs
    ADD COLUMN IF NOT EXISTS policy_id BIGINT REFERENCES vps_backup_policies(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS customer_account_id BIGINT REFERENCES customer_accounts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS scheduled_for TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS finished_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_backup_jobs_idempotency
    ON backup_jobs (idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_backup_jobs_customer
    ON backup_jobs (customer_account_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_backup_jobs_one_active_source
    ON backup_jobs (source_type, source_id)
    WHERE status IN ('pending', 'running', 'uploading');
CREATE INDEX IF NOT EXISTS idx_backup_jobs_running
    ON backup_jobs (status, updated_at)
    WHERE status IN ('pending', 'running', 'uploading');
