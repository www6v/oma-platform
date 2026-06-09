CREATE TABLE IF NOT EXISTS tenant (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  "createdAt" INTEGER NOT NULL,
  "updatedAt" INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS membership (
  user_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_membership_user ON membership (user_id);
CREATE INDEX IF NOT EXISTS idx_membership_tenant ON membership (tenant_id);

INSERT OR IGNORE INTO tenant (id, name, "createdAt", "updatedAt")
VALUES ('default', 'Default', 0, 0);
