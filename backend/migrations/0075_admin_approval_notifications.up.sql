ALTER TABLE email_outbox ADD COLUMN IF NOT EXISTS deduplication_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS email_outbox_deduplication_key_idx ON email_outbox(deduplication_key);
