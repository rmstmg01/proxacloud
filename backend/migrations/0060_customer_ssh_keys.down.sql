ALTER TABLE clusters
    DROP COLUMN IF EXISTS customer_ssh_key_id,
    DROP COLUMN IF EXISTS automation_ssh_key_pair_id;

ALTER TABLE vps_instances
    DROP COLUMN IF EXISTS customer_ssh_key_id;

DROP TABLE IF EXISTS customer_ssh_public_keys;
DROP INDEX IF EXISTS ssh_key_pairs_one_automation_default_idx;
ALTER TABLE ssh_key_pairs DROP COLUMN IF EXISTS is_automation_default;
