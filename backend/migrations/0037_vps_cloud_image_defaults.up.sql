ALTER TABLE vps_cloud_init_images
    ADD COLUMN IF NOT EXISTS ssh_user TEXT NOT NULL DEFAULT 'ubuntu',
    ADD COLUMN IF NOT EXISTS default_cpu INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS default_memory_mb INTEGER NOT NULL DEFAULT 1024,
    ADD COLUMN IF NOT EXISTS default_disk_gb INTEGER NOT NULL DEFAULT 20,
    ADD COLUMN IF NOT EXISTS source_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS detected_node_name TEXT NOT NULL DEFAULT '';

UPDATE vps_cloud_init_images
   SET ssh_user = 'almalinux',
       default_cpu = 2,
       default_memory_mb = 2048,
       default_disk_gb = 20
 WHERE lower(name) LIKE '%almalinux%'
    OR lower(image_path) LIKE '%almalinux%';
