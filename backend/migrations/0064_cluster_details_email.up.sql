ALTER TABLE clusters
    ADD COLUMN IF NOT EXISTS details_emailed_at TIMESTAMPTZ;
