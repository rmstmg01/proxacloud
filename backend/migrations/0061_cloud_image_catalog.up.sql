ALTER TABLE vps_cloud_init_images
    ADD COLUMN IF NOT EXISTS distribution TEXT NOT NULL DEFAULT 'other',
    ADD COLUMN IF NOT EXISTS version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS architecture TEXT NOT NULL DEFAULT 'amd64',
    ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS logo_key TEXT NOT NULL DEFAULT 'linux';

UPDATE vps_cloud_init_images
   SET distribution = CASE
           WHEN lower(image_path) LIKE '%almalinux%' THEN 'almalinux'
           WHEN lower(image_path) LIKE '%rocky%' THEN 'rocky'
           WHEN lower(image_path) LIKE '%ubuntu%' THEN 'ubuntu'
           WHEN lower(image_path) LIKE '%debian%' THEN 'debian'
           WHEN lower(image_path) LIKE '%fedora%' THEN 'fedora'
           ELSE 'other'
       END,
       architecture = CASE
           WHEN lower(image_path) ~ '(arm64|aarch64)' THEN 'arm64'
           ELSE 'amd64'
       END,
       logo_key = CASE
           WHEN lower(image_path) LIKE '%almalinux%' THEN 'almalinux'
           WHEN lower(image_path) LIKE '%rocky%' THEN 'rocky'
           WHEN lower(image_path) LIKE '%ubuntu%' THEN 'ubuntu'
           WHEN lower(image_path) LIKE '%debian%' THEN 'debian'
           WHEN lower(image_path) LIKE '%fedora%' THEN 'fedora'
           ELSE 'linux'
       END,
       display_name = CASE WHEN display_name = '' THEN name ELSE display_name END,
       ssh_user = 'root';

UPDATE vps_cloud_init_images
   SET version = CASE distribution
           WHEN 'ubuntu' THEN COALESCE(substring(lower(image_path) FROM 'ubuntu[^0-9]*([0-9]{2}\.[0-9]{2})'), '')
           WHEN 'debian' THEN COALESCE(substring(lower(image_path) FROM 'debian[^0-9]*([0-9]{1,2})'), '')
           WHEN 'almalinux' THEN COALESCE(substring(lower(image_path) FROM 'almalinux[^0-9]*([0-9]{1,2})'), '')
           WHEN 'rocky' THEN COALESCE(substring(lower(image_path) FROM 'rocky[^0-9]*([0-9]{1,2})'), '')
           WHEN 'fedora' THEN COALESCE(substring(lower(image_path) FROM 'fedora[^0-9]*([0-9]{1,2})'), '')
           ELSE ''
       END;

UPDATE vps_cloud_init_images
   SET display_name = CASE distribution
           WHEN 'almalinux' THEN 'AlmaLinux' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'rocky' THEN 'Rocky Linux' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'ubuntu' THEN 'Ubuntu' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'debian' THEN 'Debian' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'fedora' THEN 'Fedora' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           ELSE name
       END;

CREATE INDEX IF NOT EXISTS vps_cloud_init_images_distribution_version_idx
    ON vps_cloud_init_images (distribution, version, name);
