CREATE TABLE deploy_tokens (
  id           TEXT PRIMARY KEY,
  account_id   TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  token_hash   TEXT NOT NULL UNIQUE,
  created_by   TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX deploy_tokens_account ON deploy_tokens(account_id, created_at);

CREATE TABLE releases (
  id             TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  number         INTEGER NOT NULL,
  image          TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  requested_by   TEXT NOT NULL DEFAULT '',
  run_id         TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL
);
CREATE INDEX releases_environment ON releases(environment_id, number DESC);

ALTER TABLE environments ADD COLUMN image TEXT NOT NULL DEFAULT '';
