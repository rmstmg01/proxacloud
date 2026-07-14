UPDATE vps_cloud_init_images
   SET version = version || '.0'
 WHERE distribution IN ('almalinux', 'rocky', 'ubuntu', 'debian', 'fedora')
   AND version ~ '^[0-9]+$';

UPDATE vps_cloud_init_images
   SET display_name = CASE distribution
           WHEN 'almalinux' THEN 'AlmaLinux' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'rocky' THEN 'Rocky Linux' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'ubuntu' THEN 'Ubuntu' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'debian' THEN 'Debian' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           WHEN 'fedora' THEN 'Fedora' || CASE WHEN version = '' THEN '' ELSE ' ' || version END
           ELSE display_name
       END;
