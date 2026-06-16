ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS customer_account_id BIGINT REFERENCES customer_accounts(id);

ALTER TABLE container_instances
    ADD COLUMN IF NOT EXISTS customer_account_id BIGINT REFERENCES customer_accounts(id);

ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS customer_account_id BIGINT REFERENCES customer_accounts(id);

ALTER TABLE database_clusters
    ADD COLUMN IF NOT EXISTS customer_account_id BIGINT REFERENCES customer_accounts(id);

CREATE INDEX IF NOT EXISTS idx_vps_instances_customer_account_id ON vps_instances(customer_account_id);
CREATE INDEX IF NOT EXISTS idx_container_instances_customer_account_id ON container_instances(customer_account_id);
CREATE INDEX IF NOT EXISTS idx_clusters_customer_account_id ON clusters(customer_account_id);
CREATE INDEX IF NOT EXISTS idx_database_clusters_customer_account_id ON database_clusters(customer_account_id);
