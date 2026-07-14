ALTER TABLE ssh_key_pairs
    ADD COLUMN IF NOT EXISTS is_automation_default BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE ssh_key_pairs
   SET is_automation_default = TRUE
 WHERE id = (
       SELECT id
         FROM ssh_key_pairs
        ORDER BY
            CASE WHEN lower(name) ~ '(terraform|ansible|automation)' THEN 0 ELSE 1 END,
            id ASC
        LIMIT 1
   )
   AND NOT EXISTS (
       SELECT 1 FROM ssh_key_pairs WHERE is_automation_default = TRUE
   );

CREATE UNIQUE INDEX IF NOT EXISTS ssh_key_pairs_one_automation_default_idx
    ON ssh_key_pairs (is_automation_default)
    WHERE is_automation_default = TRUE;

CREATE TABLE IF NOT EXISTS customer_ssh_public_keys (
    id BIGSERIAL PRIMARY KEY,
    customer_account_id BIGINT REFERENCES customer_accounts(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    ssh_user TEXT NOT NULL DEFAULT 'ubuntu',
    public_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    created_by_username TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS customer_ssh_public_keys_account_fingerprint_idx
    ON customer_ssh_public_keys (COALESCE(customer_account_id, 0), fingerprint);

ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS customer_ssh_key_id BIGINT REFERENCES customer_ssh_public_keys(id) ON DELETE SET NULL;

ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS automation_ssh_key_pair_id BIGINT REFERENCES ssh_key_pairs(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS customer_ssh_key_id BIGINT REFERENCES customer_ssh_public_keys(id) ON DELETE SET NULL;
