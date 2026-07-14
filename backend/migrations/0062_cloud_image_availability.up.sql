ALTER TABLE vps_cloud_init_images
    ADD COLUMN IF NOT EXISTS available BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS vps_cloud_init_images_available_distribution_idx
    ON vps_cloud_init_images (available, distribution, version, name);
