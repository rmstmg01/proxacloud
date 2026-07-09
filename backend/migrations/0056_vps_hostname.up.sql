ALTER TABLE vps_instances
    ADD COLUMN IF NOT EXISTS hostname TEXT NOT NULL DEFAULT '';

UPDATE vps_instances
   SET hostname = vm_name
 WHERE hostname = '';
