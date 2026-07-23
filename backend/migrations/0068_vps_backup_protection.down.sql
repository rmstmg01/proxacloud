DROP INDEX IF EXISTS idx_backup_jobs_running;
DROP INDEX IF EXISTS idx_backup_jobs_one_active_source;
DROP INDEX IF EXISTS idx_backup_jobs_customer;
DROP INDEX IF EXISTS idx_backup_jobs_idempotency;

ALTER TABLE backup_jobs
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS attempt,
    DROP COLUMN IF EXISTS finished_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS scheduled_for,
    DROP COLUMN IF EXISTS trigger_type,
    DROP COLUMN IF EXISTS customer_account_id,
    DROP COLUMN IF EXISTS policy_id;

DROP TABLE IF EXISTS vps_backup_policies;

DELETE FROM billing_prices
WHERE resource_type IN ('vps_backup_monthly', 'vps_backup_weekly', 'vps_backup_daily');
