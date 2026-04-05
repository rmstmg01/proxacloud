ALTER TABLE cluster_nodes
    ADD COLUMN IF NOT EXISTS proxmox_credential_id BIGINT REFERENCES proxmox_credentials(id);

UPDATE cluster_nodes cn
SET proxmox_credential_id = c.proxmox_credential_id
FROM clusters c
WHERE c.id = cn.cluster_id
  AND cn.proxmox_credential_id IS NULL
  AND c.proxmox_credential_id IS NOT NULL;
