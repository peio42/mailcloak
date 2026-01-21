CREATE TABLE IF NOT EXISTS aliases (
  alias_email TEXT PRIMARY KEY,
  username    TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  updated_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX IF NOT EXISTS idx_aliases_username ON aliases(username);

CREATE TABLE IF NOT EXISTS apps (
  app_id      TEXT PRIMARY KEY,
  secret_hash TEXT NOT NULL,
  enabled     INTEGER NOT NULL DEFAULT 1,
  created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS app_from (
  app_id    TEXT NOT NULL,
  from_addr TEXT NOT NULL,
  enabled   INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY (app_id, from_addr),
  FOREIGN KEY (app_id) REFERENCES apps(app_id) ON DELETE CASCADE
);