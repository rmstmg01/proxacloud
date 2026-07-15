DROP INDEX IF EXISTS idx_database_clusters_region_id;
DROP INDEX IF EXISTS idx_clusters_region_id;

ALTER TABLE database_clusters DROP COLUMN IF EXISTS region_id;
ALTER TABLE clusters DROP COLUMN IF EXISTS region_id;
