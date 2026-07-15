DROP INDEX IF EXISTS database_clusters_cloud_init_image_id_idx;

ALTER TABLE database_clusters
    DROP COLUMN IF EXISTS details_emailed_at,
    DROP COLUMN IF EXISTS passwords_generated,
    DROP COLUMN IF EXISTS cloud_init_image_path,
    DROP COLUMN IF EXISTS cloud_init_image_id;
