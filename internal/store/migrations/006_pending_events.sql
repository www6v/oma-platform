CREATE TABLE pending_events (
  session_id TEXT NOT NULL,
  pending_seq INTEGER NOT NULL,
  enqueued_at INTEGER NOT NULL,
  session_thread_id TEXT NOT NULL DEFAULT 'sthr_primary',
  type TEXT NOT NULL,
  event_id TEXT NOT NULL,
  data TEXT NOT NULL,
  cancelled_at INTEGER,
  PRIMARY KEY (session_id, pending_seq)
);

CREATE INDEX idx_pending_events_session_thread
  ON pending_events(session_id, session_thread_id, pending_seq);
