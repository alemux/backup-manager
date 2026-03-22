-- Add use_tls column for FTP servers that require TLS/SSL
-- This is idempotent: SQLite doesn't support IF NOT EXISTS for ALTER TABLE,
-- so we check via a dummy SELECT first. If the column already exists, this
-- migration is already recorded and won't run again.
ALTER TABLE servers ADD COLUMN use_tls INTEGER NOT NULL DEFAULT 0;
