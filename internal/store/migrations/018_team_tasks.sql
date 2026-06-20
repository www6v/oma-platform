CREATE TABLE IF NOT EXISTS team_tasks (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  subject         TEXT NOT NULL,
  description     TEXT,
  active_form     TEXT,
  owner_member_id TEXT,
  status          TEXT NOT NULL DEFAULT 'pending',
  blocks_json     TEXT NOT NULL DEFAULT '[]',
  blocked_by_json TEXT NOT NULL DEFAULT '[]',
  metadata_json   TEXT,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_team_tasks_team
  ON team_tasks (team_id, status);
