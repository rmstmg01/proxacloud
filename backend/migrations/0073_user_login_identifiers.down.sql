BEGIN;
DROP TRIGGER IF EXISTS users_sync_login_identifiers ON users;
DROP FUNCTION IF EXISTS sync_user_login_identifiers();
DROP TABLE IF EXISTS user_login_identifiers;
DROP INDEX IF EXISTS users_email_normalized_key;
DROP INDEX IF EXISTS users_username_normalized_key;
COMMIT;
