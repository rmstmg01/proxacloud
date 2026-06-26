CREATE TABLE IF NOT EXISTS network_ip_detections (
    id BIGSERIAL PRIMARY KEY,
    network_profile_id BIGINT NOT NULL REFERENCES network_profiles(id) ON DELETE CASCADE,
    ip_address TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT 'icmp',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (network_profile_id, ip_address)
);

CREATE INDEX IF NOT EXISTS idx_network_ip_detections_profile_seen
    ON network_ip_detections (network_profile_id, last_seen_at DESC);
