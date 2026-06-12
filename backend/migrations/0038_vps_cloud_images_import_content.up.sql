WITH candidates AS (
    SELECT
        id,
        regexp_replace(image_path, '^local:iso/', 'local:import/') AS target_image_path,
        CASE
            WHEN source_path ~* '^local:iso/' THEN regexp_replace(source_path, '^local:iso/', 'local:import/')
            ELSE source_path
        END AS target_source_path
    FROM vps_cloud_init_images
    WHERE image_path ~* '^local:iso/.*\.(qcow2|img|raw)$'
)
UPDATE vps_cloud_init_images AS image
   SET image_path = candidates.target_image_path,
       source_path = candidates.target_source_path,
       description = CASE
           WHEN image.description = 'Default Ubuntu 24.04 cloud-init image path'
               THEN 'Default Ubuntu 24.04 cloud-init import image path'
           WHEN image.description LIKE 'Discovered from Proxmox local:iso%'
               THEN replace(image.description, 'local:iso', 'local:import')
           ELSE image.description
       END,
       updated_at = NOW()
  FROM candidates
 WHERE image.id = candidates.id
   AND NOT EXISTS (
       SELECT 1
         FROM vps_cloud_init_images AS existing
        WHERE existing.id <> image.id
          AND existing.image_path = candidates.target_image_path
   );

UPDATE vps_instances
   SET cloud_init_image_path = regexp_replace(cloud_init_image_path, '^local:iso/', 'local:import/'),
       updated_at = NOW()
 WHERE cloud_init_image_path ~* '^local:iso/.*\.(qcow2|img|raw)$';

UPDATE vps_cloud_init_images
   SET os_type = 'almalinux',
       ssh_user = 'almalinux',
       default_cpu = GREATEST(default_cpu, 2),
       default_memory_mb = GREATEST(default_memory_mb, 2048),
       default_disk_gb = GREATEST(default_disk_gb, 20),
       updated_at = NOW()
 WHERE lower(name) LIKE '%almalinux%'
    OR lower(image_path) LIKE '%almalinux%'
    OR lower(source_path) LIKE '%almalinux%';
