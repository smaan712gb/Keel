CREATE TABLE IF NOT EXISTS locks (
    id                TEXT    PRIMARY KEY,
    repo_root         TEXT    NOT NULL,
    file_path         TEXT    NOT NULL,
    agent_id          TEXT    NOT NULL,
    plan_summary      TEXT    NOT NULL DEFAULT '',
    eta_seconds       INTEGER,
    acquired_at       TEXT    NOT NULL,
    lease_expires_at  TEXT    NOT NULL,
    released_at       TEXT
);

CREATE INDEX IF NOT EXISTS idx_locks_active
    ON locks (repo_root, file_path, released_at, lease_expires_at);

CREATE INDEX IF NOT EXISTS idx_locks_agent
    ON locks (repo_root, agent_id, released_at);

CREATE TABLE IF NOT EXISTS plans (
    id            TEXT    PRIMARY KEY,
    repo_root     TEXT    NOT NULL,
    agent_id      TEXT    NOT NULL,
    summary       TEXT    NOT NULL,
    files_json    TEXT    NOT NULL DEFAULT '[]',
    declared_at   TEXT    NOT NULL,
    completed_at  TEXT
);

CREATE INDEX IF NOT EXISTS idx_plans_active
    ON plans (repo_root, agent_id, completed_at);
