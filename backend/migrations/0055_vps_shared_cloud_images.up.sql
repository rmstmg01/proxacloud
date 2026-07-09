WITH local_images AS (
    SELECT
        id,
        image_path,
        regexp_replace(image_path, '^local:import/', 'cloud-images:import/') AS shared_image_path,
        is_default
    FROM vps_cloud_init_images
    WHERE image_path ~* '^local:import/'
)
UPDATE vps_cloud_init_images AS shared
   SET is_default = shared.is_default OR local_images.is_default,
       updated_at = NOW()
  FROM local_images
 WHERE shared.image_path = local_images.shared_image_path;

WITH local_images AS (
    SELECT
        id,
        regexp_replace(image_path, '^local:import/', 'cloud-images:import/') AS shared_image_path,
        CASE
            WHEN source_path ~* '^local:import/' THEN regexp_replace(source_path, '^local:import/', 'cloud-images:import/')
            ELSE source_path
        END AS shared_source_path
    FROM vps_cloud_init_images
    WHERE image_path ~* '^local:import/'
)
UPDATE vps_cloud_init_images AS image
   SET image_path = local_images.shared_image_path,
       source_path = local_images.shared_source_path,
       description = CASE
           WHEN image.description LIKE '%local:import%'
               THEN replace(image.description, 'local:import', 'cloud-images:import')
           ELSE image.description
       END,
       updated_at = NOW()
  FROM local_images
 WHERE image.id = local_images.id
   AND NOT EXISTS (
       SELECT 1
         FROM vps_cloud_init_images AS existing
        WHERE existing.id <> image.id
          AND existing.image_path = local_images.shared_image_path
   );

UPDATE vps_cloud_init_images
   SET is_default = FALSE,
       updated_at = NOW()
 WHERE image_path ~* '^local:import/';
