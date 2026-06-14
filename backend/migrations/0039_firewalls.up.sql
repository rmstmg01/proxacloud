CREATE TABLE IF NOT EXISTS firewall_groups (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    default_inbound_policy TEXT NOT NULL DEFAULT 'deny' CHECK (default_inbound_policy IN ('allow', 'deny')),
    default_outbound_policy TEXT NOT NULL DEFAULT 'allow' CHECK (default_outbound_policy IN ('allow', 'deny')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'created',
    last_applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS firewall_rules (
    id BIGSERIAL PRIMARY KEY,
    firewall_group_id BIGINT NOT NULL REFERENCES firewall_groups(id) ON DELETE CASCADE,
    direction TEXT NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    action TEXT NOT NULL CHECK (action IN ('allow', 'deny')),
    protocol TEXT NOT NULL CHECK (protocol IN ('tcp', 'udp', 'icmp', 'any')),
    source_cidr TEXT NOT NULL DEFAULT '',
    destination_cidr TEXT NOT NULL DEFAULT '',
    port_start INTEGER,
    port_end INTEGER,
    priority INTEGER NOT NULL DEFAULT 100,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (firewall_group_id, priority)
);

CREATE TABLE IF NOT EXISTS firewall_attachments (
    id BIGSERIAL PRIMARY KEY,
    firewall_group_id BIGINT NOT NULL REFERENCES firewall_groups(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('vps', 'container', 'cluster_node', 'database_node')),
    target_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (firewall_group_id, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS firewall_events (
    id BIGSERIAL PRIMARY KEY,
    firewall_group_id BIGINT NOT NULL REFERENCES firewall_groups(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    level TEXT NOT NULL,
    message TEXT NOT NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
