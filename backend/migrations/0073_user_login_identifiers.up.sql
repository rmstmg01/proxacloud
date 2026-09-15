BEGIN;
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;

-- Refuse ambiguous existing identities; never choose or delete an account.
DO $$
BEGIN
    IF EXISTS (
        SELECT identifier FROM (
            SELECT id, lower(btrim(username)) AS identifier FROM users
            UNION
            SELECT id, lower(btrim(email)) FROM users WHERE btrim(email) <> ''
        ) identities GROUP BY identifier HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'Duplicate login identifiers exist. Run scripts/audit-login-identifiers.sql and resolve conflicting accounts before retrying migration 0073.';
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_key ON users (lower(btrim(username)));
CREATE UNIQUE INDEX IF NOT EXISTS users_email_normalized_key ON users (lower(btrim(email))) WHERE btrim(email) <> '';

-- One shared namespace also prevents a username from claiming another user's email.
-- The primary key arbitrates concurrent writes, including cross-column collisions.
CREATE TABLE IF NOT EXISTS user_login_identifiers (
    identifier TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS user_login_identifiers_user_idx ON user_login_identifiers(user_id);

CREATE OR REPLACE FUNCTION sync_user_login_identifiers() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    login_value TEXT;
    owner_id BIGINT;
BEGIN
    DELETE FROM user_login_identifiers WHERE user_id = NEW.id
      AND identifier <> lower(btrim(NEW.username))
      AND identifier <> coalesce(lower(btrim(NEW.email)), '');
    FOR login_value IN
        SELECT DISTINCT value FROM unnest(ARRAY[lower(btrim(NEW.username)), lower(btrim(NEW.email))]) AS values_to_claim(value)
        WHERE value <> '' ORDER BY value
    LOOP
        INSERT INTO user_login_identifiers(identifier, user_id) VALUES (login_value, NEW.id)
        ON CONFLICT (identifier) DO UPDATE SET user_id = user_login_identifiers.user_id
        RETURNING user_id INTO owner_id;
        IF owner_id <> NEW.id THEN
            IF login_value = lower(btrim(NEW.username)) THEN
                RAISE EXCEPTION 'Username already in use' USING ERRCODE = '23505', CONSTRAINT = 'users_username_normalized_key';
            ELSE
                RAISE EXCEPTION 'Email already in use' USING ERRCODE = '23505', CONSTRAINT = 'users_email_normalized_key';
            END IF;
        END IF;
    END LOOP;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS users_sync_login_identifiers ON users;
CREATE TRIGGER users_sync_login_identifiers AFTER INSERT OR UPDATE OF username, email ON users
FOR EACH ROW EXECUTE FUNCTION sync_user_login_identifiers();

INSERT INTO user_login_identifiers(identifier, user_id)
SELECT lower(btrim(username)), id FROM users WHERE btrim(username) <> ''
UNION SELECT lower(btrim(email)), id FROM users WHERE btrim(email) <> ''
ON CONFLICT (identifier) DO NOTHING;
COMMIT;
