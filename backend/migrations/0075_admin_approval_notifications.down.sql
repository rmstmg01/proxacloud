DROP INDEX IF EXISTS email_outbox_deduplication_key_idx;
ALTER TABLE email_outbox DROP COLUMN IF EXISTS deduplication_key;
