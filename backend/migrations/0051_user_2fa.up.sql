ALTER TABLE users
    ADD COLUMN IF NOT EXISTS two_factor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS two_factor_method TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS two_factor_secret TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS two_factor_pending_secret TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS two_factor_enabled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_login_ip TEXT NOT NULL DEFAULT '';

UPDATE users
SET two_factor_method = CASE
    WHEN two_factor_enabled = TRUE AND two_factor_secret <> '' THEN 'totp'
    WHEN two_factor_enabled = TRUE THEN 'email'
    ELSE 'none'
END
WHERE two_factor_method IS NULL OR two_factor_method = '';

CREATE TABLE IF NOT EXISTS user_2fa_challenges (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    method TEXT NOT NULL,
    challenge_token TEXT NOT NULL UNIQUE,
    code_hash TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_2fa_challenges_user ON user_2fa_challenges(user_id);
CREATE INDEX IF NOT EXISTS idx_user_2fa_challenges_purpose ON user_2fa_challenges(purpose);
