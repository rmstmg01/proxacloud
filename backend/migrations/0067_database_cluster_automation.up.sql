ALTER TABLE database_clusters
    ADD COLUMN IF NOT EXISTS cloud_init_image_id BIGINT REFERENCES vps_cloud_init_images(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS cloud_init_image_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS passwords_generated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS details_emailed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS database_clusters_cloud_init_image_id_idx
    ON database_clusters (cloud_init_image_id);
