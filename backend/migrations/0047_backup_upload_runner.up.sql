ALTER TABLE backup_jobs
    ADD COLUMN IF NOT EXISTS staging_storage_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS artifact_volid TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS artifact_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upload_method TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upload_status TEXT NOT NULL DEFAULT 'not_required',
    ADD COLUMN IF NOT EXISTS remote_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upload_output TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upload_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_backup_jobs_upload_status ON backup_jobs(upload_status);
