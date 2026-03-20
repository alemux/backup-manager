-- migrations/002_add_exclude_patterns.sql
ALTER TABLE backup_sources ADD COLUMN exclude_patterns TEXT NOT NULL DEFAULT '';
