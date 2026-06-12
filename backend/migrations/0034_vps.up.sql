CREATE TABLE IF NOT EXISTS vps_cloud_init_images (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    image_path TEXT NOT NULL UNIQUE,
    os_type TEXT NOT NULL DEFAULT 'ubuntu',
    description TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO vps_cloud_init_images (name, image_path, os_type, description, is_default)
VALUES (
    'Ubuntu 24.04 Cloud Image',
    'local:import/ubuntu-24.04-server-cloudimg-amd64.img',
    'ubuntu',
    'Default Ubuntu 24.04 cloud-init import image path',
    TRUE
)
ON CONFLICT (image_path) DO NOTHING;

CREATE TABLE IF NOT EXISTS vps_instances (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    proxmox_credential_id BIGINT NOT NULL REFERENCES proxmox_credentials(id),
    proxmox_node_name TEXT NOT NULL,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id),
    cloud_init_image_id BIGINT REFERENCES vps_cloud_init_images(id),
    cloud_init_image_path TEXT NOT NULL,
    ssh_key_pair_id BIGINT REFERENCES ssh_key_pairs(id) ON DELETE SET NULL,
    vm_name TEXT NOT NULL,
    ip_mode TEXT NOT NULL DEFAULT 'dhcp',
    ip_address TEXT NOT NULL DEFAULT '',
    cpu INTEGER NOT NULL DEFAULT 1,
    memory_mb INTEGER NOT NULL DEFAULT 1024,
    disk_gb INTEGER NOT NULL DEFAULT 20,
    ssh_user TEXT NOT NULL DEFAULT 'ubuntu',
    root_password_secret_ref TEXT NOT NULL DEFAULT '',
    terraform_work_dir TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS vps_events (
    id BIGSERIAL PRIMARY KEY,
    vps_id BIGINT NOT NULL REFERENCES vps_instances(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
