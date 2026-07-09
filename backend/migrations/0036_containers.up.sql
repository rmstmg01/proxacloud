CREATE TABLE IF NOT EXISTS container_templates (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    template_path TEXT NOT NULL UNIQUE,
    os_type TEXT NOT NULL DEFAULT 'debian',
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO container_templates (name, template_path, os_type, description, is_default)
SELECT
    'Debian 12 LXC Template',
    'local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst',
    'debian',
    'Default Debian 12 LXC template path',
    TRUE
WHERE NOT EXISTS (
    SELECT 1
      FROM container_templates
     WHERE name = 'Debian 12 LXC Template'
        OR template_path = 'local:vztmpl/debian-12-standard_12.7-1_amd64.tar.zst'
);

CREATE TABLE IF NOT EXISTS container_instances (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    proxmox_credential_id BIGINT NOT NULL REFERENCES proxmox_credentials(id),
    proxmox_node_name TEXT NOT NULL,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id),
    template_id BIGINT REFERENCES container_templates(id),
    template_path TEXT NOT NULL,
    ssh_key_pair_id BIGINT REFERENCES ssh_key_pairs(id) ON DELETE SET NULL,
    container_name TEXT NOT NULL,
    ip_mode TEXT NOT NULL DEFAULT 'dhcp',
    ip_address TEXT NOT NULL DEFAULT '',
    cpu INTEGER NOT NULL DEFAULT 1,
    memory_mb INTEGER NOT NULL DEFAULT 512,
    disk_gb INTEGER NOT NULL DEFAULT 8,
    unprivileged BOOLEAN NOT NULL DEFAULT TRUE,
    nesting BOOLEAN NOT NULL DEFAULT TRUE,
    root_password_secret_ref TEXT NOT NULL DEFAULT '',
    terraform_work_dir TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS container_events (
    id BIGSERIAL PRIMARY KEY,
    container_id BIGINT NOT NULL REFERENCES container_instances(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
