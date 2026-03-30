ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS proxmox_credential_id BIGINT REFERENCES proxmox_credentials(id);
