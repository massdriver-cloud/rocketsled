CREATE TABLE users (
  id               TEXT PRIMARY KEY,
  provider         TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  login            TEXT NOT NULL,
  name             TEXT NOT NULL DEFAULT '',
  email            TEXT NOT NULL DEFAULT '',
  avatar_url       TEXT NOT NULL DEFAULT '',
  created_at       TEXT NOT NULL
);

CREATE TABLE accounts (
  id                  TEXT PRIMARY KEY,
  name                TEXT NOT NULL,
  owner_user_id       TEXT NOT NULL UNIQUE REFERENCES users(id),
  organization_id     TEXT NOT NULL,
  organization_name   TEXT NOT NULL DEFAULT '',
  status              TEXT NOT NULL,
  catalog_id          TEXT NOT NULL DEFAULT '',
  project_id          TEXT NOT NULL DEFAULT '',
  base_environment_id TEXT NOT NULL DEFAULT '',
  platforms           TEXT NOT NULL DEFAULT '{}',
  install_log         TEXT NOT NULL DEFAULT '',
  install_error       TEXT NOT NULL DEFAULT '',
  credential_url      TEXT NOT NULL DEFAULT '',
  credential_enc      BLOB NOT NULL,
  bootstrap_state     BLOB,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE TABLE environments (
  id             TEXT PRIMARY KEY,
  account_id     TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  slug           TEXT NOT NULL,
  name           TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  presets        TEXT NOT NULL DEFAULT '{}',
  state          TEXT NOT NULL,
  created_by     TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  UNIQUE (account_id, slug)
);

CREATE TABLE runs (
  id             TEXT PRIMARY KEY,
  environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  number         INTEGER NOT NULL,
  trigger_kind   TEXT NOT NULL,
  status         TEXT NOT NULL,
  message        TEXT NOT NULL DEFAULT '',
  requested_by   TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX runs_environment ON runs(environment_id, number DESC);
