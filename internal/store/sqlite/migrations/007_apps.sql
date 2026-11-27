CREATE TABLE apps (
  id         TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  runtime    TEXT NOT NULL,
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX apps_account ON apps(account_id, created_at);

ALTER TABLE environments ADD COLUMN app TEXT NOT NULL DEFAULT '';
