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

CREATE TABLE credentials (
  user_id    TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  token_enc  BLOB NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE stacks (
  id                TEXT PRIMARY KEY,
  name              TEXT NOT NULL,
  repo_provider     TEXT NOT NULL,
  repo_external_id  TEXT NOT NULL,
  repo_full_name    TEXT NOT NULL,
  repo_url          TEXT NOT NULL,
  repo_default_branch TEXT NOT NULL,
  repo_private      INTEGER NOT NULL DEFAULT 0,
  project_id        TEXT NOT NULL,
  webhook_id        TEXT NOT NULL DEFAULT '',
  deploy_token_hash TEXT NOT NULL,
  created_by        TEXT NOT NULL,
  created_at        TEXT NOT NULL
);
CREATE INDEX stacks_repo ON stacks(repo_provider, repo_full_name);

CREATE TABLE instances (
  id             TEXT PRIMARY KEY,
  stack_id       TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
  slug           TEXT NOT NULL,
  name           TEXT NOT NULL,
  branch         TEXT NOT NULL,
  environment_id TEXT NOT NULL,
  presets        TEXT NOT NULL DEFAULT '{}',
  state          TEXT NOT NULL,
  created_by     TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  UNIQUE (stack_id, slug)
);

CREATE TABLE deploys (
  id             TEXT PRIMARY KEY,
  instance_id    TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  number         INTEGER NOT NULL,
  trigger_kind   TEXT NOT NULL,
  ref            TEXT NOT NULL,
  sha            TEXT NOT NULL,
  image_tag      TEXT NOT NULL,
  status         TEXT NOT NULL,
  message        TEXT NOT NULL DEFAULT '',
  platform_ids   TEXT NOT NULL DEFAULT '[]',
  requested_by   TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL
);
CREATE INDEX deploys_instance ON deploys(instance_id, number DESC);
