CREATE TABLE IF NOT EXISTS session_wakeup_schedules (
  id              TEXT PRIMARY KEY NOT NULL,
  tenant_id       TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  prompt          TEXT NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('one_shot', 'cron')),
  cron            TEXT,
  fire_at         INTEGER NOT NULL,
  parent_event_id TEXT,
  span_event_id   TEXT,
  scheduled_at    TEXT NOT NULL,
  created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wakeup_fire_at
  ON session_wakeup_schedules (fire_at);

CREATE INDEX IF NOT EXISTS idx_wakeup_session
  ON session_wakeup_schedules (session_id);
