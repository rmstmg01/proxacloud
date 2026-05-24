CREATE TABLE IF NOT EXISTS database_clusters (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    engine TEXT NOT NULL DEFAULT 'mariadb-galera',
    mariadb_version TEXT NOT NULL DEFAULT '11.4',
    topology_type TEXT NOT NULL DEFAULT 'tri',
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id),
    proxmox_credential_id BIGINT NOT NULL REFERENCES proxmox_credentials(id),
    ssh_key_pair_id BIGINT REFERENCES ssh_key_pairs(id) ON DELETE SET NULL,
    database_name TEXT NOT NULL,
    app_username TEXT NOT NULL,
    root_password_secret_ref TEXT NOT NULL DEFAULT '',
    app_password_secret_ref TEXT NOT NULL DEFAULT '',
    sst_username TEXT NOT NULL DEFAULT 'sstuser',
    sst_password_secret_ref TEXT NOT NULL DEFAULT '',
    haproxy_namespace TEXT NOT NULL DEFAULT 'pscs',
    haproxy_service_name TEXT NOT NULL DEFAULT 'external-mariadb',
    terraform_work_dir TEXT NOT NULL DEFAULT '',
    ansible_work_dir TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'planned',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS database_cluster_nodes (
    id BIGSERIAL PRIMARY KEY,
    database_cluster_id BIGINT NOT NULL REFERENCES database_clusters(id) ON DELETE CASCADE,
    node_index INTEGER NOT NULL,
    vm_name TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    proxmox_node_name TEXT NOT NULL,
    proxmox_credential_id BIGINT NOT NULL REFERENCES proxmox_credentials(id),
    cpu INTEGER NOT NULL,
    memory_mb INTEGER NOT NULL,
    disk_gb INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (database_cluster_id, node_index),
    UNIQUE (vm_name)
);

CREATE TABLE IF NOT EXISTS database_cluster_runs (
    id BIGSERIAL PRIMARY KEY,
    database_cluster_id BIGINT NOT NULL REFERENCES database_clusters(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    status TEXT NOT NULL,
    work_dir TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_database_cluster_nodes_cluster
    ON database_cluster_nodes (database_cluster_id, node_index);

CREATE INDEX IF NOT EXISTS idx_database_cluster_runs_cluster
    ON database_cluster_runs (database_cluster_id, created_at DESC);
