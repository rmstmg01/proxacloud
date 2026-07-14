DROP INDEX IF EXISTS vps_cloud_init_images_distribution_version_idx;
ALTER TABLE vps_cloud_init_images
    DROP COLUMN IF EXISTS logo_key,
    DROP COLUMN IF EXISTS display_name,
    DROP COLUMN IF EXISTS architecture,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS distribution;
