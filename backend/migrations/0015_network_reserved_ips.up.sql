CREATE TABLE IF NOT EXISTS network_reserved_ips (
    id BIGSERIAL PRIMARY KEY,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL,
    ip_address TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (network_profile_id, ip_address)
);
