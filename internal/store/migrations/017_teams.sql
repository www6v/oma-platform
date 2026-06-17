CREATE TABLE IF NOT EXISTS teams (
  id              TEXT PRIMARY KEY NOT NULL,
  session_id      TEXT NOT NULL,
  tenant_id       TEXT NOT NULL,
  name            TEXT NOT NULL,
  description     TEXT,
  lead_thread_id  TEXT NOT NULL DEFAULT 'sthr_primary',
  lead_agent_id   TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  created_at      INTEGER NOT NULL,
  UNIQUE(session_id, name)
);

CREATE INDEX IF NOT EXISTS idx_teams_session
  ON teams (session_id);

CREATE TABLE IF NOT EXISTS team_members (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  agent_id        TEXT NOT NULL,
  display_name    TEXT NOT NULL,
  color           TEXT,
  thread_id       TEXT,
  role            TEXT,
  plan_mode_required INTEGER NOT NULL DEFAULT 0,
  backend_type    TEXT NOT NULL DEFAULT 'in_process',
  status          TEXT NOT NULL DEFAULT 'idle',
  joined_at       INTEGER NOT NULL,
  UNIQUE(team_id, display_name)
);

CREATE INDEX IF NOT EXISTS idx_team_members_team
  ON team_members (team_id);

CREATE TABLE IF NOT EXISTS agent_messages (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  from_member_id  TEXT NOT NULL,
  to_member_id    TEXT,
  message_type    TEXT NOT NULL DEFAULT 'text',
  body            TEXT NOT NULL,
  summary         TEXT,
  read_at         INTEGER,
  created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_team_recipient
  ON agent_messages (team_id, to_member_id, read_at);
