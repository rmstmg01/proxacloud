DROP INDEX IF EXISTS idx_vps_events_actor;

ALTER TABLE vps_events
    DROP COLUMN IF EXISTS actor_role,
    DROP COLUMN IF EXISTS actor_username,
    DROP COLUMN IF EXISTS actor_user_id;

DROP INDEX IF EXISTS idx_vps_instances_admin_suspended;
DROP INDEX IF EXISTS idx_vps_instances_admin_owner;

ALTER TABLE vps_instances
    DROP COLUMN IF EXISTS admin_suspension_error,
    DROP COLUMN IF EXISTS admin_suspension_reason,
    DROP COLUMN IF EXISTS admin_suspended_by_user_id,
    DROP COLUMN IF EXISTS admin_suspended_at,
    DROP COLUMN IF EXISTS admin_owner_user_id,
    DROP COLUMN IF EXISTS created_by_user_id;
