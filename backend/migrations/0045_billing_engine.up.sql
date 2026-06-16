CREATE TABLE IF NOT EXISTS billing_prices (
    id BIGSERIAL PRIMARY KEY,
    resource_type TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    base_monthly_cents INTEGER NOT NULL DEFAULT 0,
    cpu_monthly_cents INTEGER NOT NULL DEFAULT 0,
    memory_gb_monthly_cents INTEGER NOT NULL DEFAULT 0,
    disk_gb_monthly_cents INTEGER NOT NULL DEFAULT 0,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO billing_prices (resource_type, display_name, base_monthly_cents, cpu_monthly_cents, memory_gb_monthly_cents, disk_gb_monthly_cents)
VALUES
    ('vps', 'VPS', 500, 300, 200, 5),
    ('container', 'Container', 250, 150, 100, 3),
    ('cluster', 'Kubernetes Cluster', 1500, 250, 180, 4),
    ('database_cluster', 'Database Cluster', 1200, 250, 180, 4),
    ('backup_storage', 'Backup Storage', 0, 0, 0, 2)
ON CONFLICT (resource_type) DO NOTHING;

CREATE TABLE IF NOT EXISTS account_credit_ledger (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES customer_accounts(id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    entry_type TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_credit_ledger_idempotency
    ON account_credit_ledger(account_id, idempotency_key)
    WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_account_credit_ledger_account_id ON account_credit_ledger(account_id);

CREATE TABLE IF NOT EXISTS payment_transactions (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES customer_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_session_id TEXT NOT NULL DEFAULT '',
    amount_cents INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'usd',
    status TEXT NOT NULL DEFAULT 'pending',
    checkout_url TEXT NOT NULL DEFAULT '',
    raw_response TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_transactions_account_id ON payment_transactions(account_id);
CREATE INDEX IF NOT EXISTS idx_payment_transactions_provider_session ON payment_transactions(provider, provider_session_id);

CREATE TABLE IF NOT EXISTS billing_invoices (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES customer_accounts(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'posted',
    subtotal_cents INTEGER NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'usd',
    source TEXT NOT NULL DEFAULT 'monthly_resource_charge',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(account_id, period_start, source)
);

CREATE TABLE IF NOT EXISTS billing_invoice_items (
    id BIGSERIAL PRIMARY KEY,
    invoice_id BIGINT NOT NULL REFERENCES billing_invoices(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id BIGINT NOT NULL,
    resource_name TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 1,
    amount_cents INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_billing_invoice_items_invoice_id ON billing_invoice_items(invoice_id);
