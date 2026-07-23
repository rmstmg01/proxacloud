ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS admin_owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS admin_suspended_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS admin_suspended_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS admin_suspension_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS admin_suspension_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_vps_instances_admin_owner
    ON vps_instances (admin_owner_user_id)
    WHERE customer_account_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_vps_instances_admin_suspended
    ON vps_instances (admin_suspended_at)
    WHERE admin_suspended_at IS NOT NULL;

ALTER TABLE vps_events
    ADD COLUMN IF NOT EXISTS actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS actor_username TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS actor_role TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_vps_events_actor
    ON vps_events (actor_user_id, created_at DESC);

COMMENT ON COLUMN vps_instances.admin_suspended_at IS
    'Independent administrative control-plane lock; never inferred from or overwritten by Proxmox power status.';
COMMENT ON COLUMN vps_instances.admin_owner_user_id IS
    'Current administrator owner when customer_account_id is NULL.';
