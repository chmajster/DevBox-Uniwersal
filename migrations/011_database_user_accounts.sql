CREATE TABLE database_accounts (
    id TEXT PRIMARY KEY,
    engine TEXT NOT NULL,
    username TEXT NOT NULL,
    secret_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(engine, username)
);

CREATE TABLE database_user_grants (
    user_id TEXT NOT NULL REFERENCES database_accounts(id) ON DELETE CASCADE,
    database_id TEXT NOT NULL REFERENCES databases(id) ON DELETE CASCADE,
    privileges_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(user_id, database_id)
);

INSERT INTO database_accounts(id,engine,username,secret_id,created_at,updated_at)
SELECT du.id,
       CASE
         WHEN lower(d.engine) IN ('mysql','mariadb') THEN 'mysql'
         WHEN lower(d.engine) IN ('postgres','postgresql') THEN 'postgresql'
         ELSE lower(d.engine)
       END,
       du.username,
       du.secret_id,
       du.created_at,
       du.updated_at
FROM database_users du
JOIN databases d ON d.id=du.database_id;

INSERT INTO database_user_grants(user_id,database_id,privileges_json,created_at,updated_at)
SELECT id,database_id,privileges_json,created_at,updated_at
FROM database_users;

DROP INDEX IF EXISTS idx_database_users_username;
DROP TABLE database_users;

CREATE INDEX idx_database_accounts_engine ON database_accounts(engine, username);
CREATE INDEX idx_database_user_grants_database ON database_user_grants(database_id, user_id);
