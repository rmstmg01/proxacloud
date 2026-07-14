DROP INDEX IF EXISTS vps_cloud_init_images_available_distribution_idx;
ALTER TABLE vps_cloud_init_images
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS available;
