-- GitHub publication-first install: app identity columns used by install bridge.

ALTER TABLE github_publications ADD COLUMN app_oma_id TEXT;
ALTER TABLE github_publications ADD COLUMN app_id TEXT;
ALTER TABLE github_publications ADD COLUMN app_slug TEXT;
ALTER TABLE github_publications ADD COLUMN bot_login TEXT;
ALTER TABLE github_publications ADD COLUMN private_key_cipher TEXT;

CREATE INDEX IF NOT EXISTS idx_github_publications_app_oma_id
  ON github_publications (app_oma_id)
  WHERE app_oma_id IS NOT NULL;
