CREATE TABLE script_apps (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    install_source TEXT NOT NULL,
    update_source TEXT NOT NULL DEFAULT '',
    uninstall_source TEXT NOT NULL DEFAULT '',
    interpreter TEXT NOT NULL DEFAULT 'bash' CHECK(interpreter IN ('bash','sh','pwsh','powershell')),
    checksum_sha256 TEXT NOT NULL DEFAULT '',
    run_as_root INTEGER NOT NULL DEFAULT 0 CHECK(run_as_root IN (0,1)),
    allow_insecure INTEGER NOT NULL DEFAULT 0 CHECK(allow_insecure IN (0,1)),
    manager TEXT NOT NULL DEFAULT 'script' CHECK(manager IN ('script','systemd','docker')),
    manager_target TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'not_installed',
    last_error TEXT NOT NULL DEFAULT '',
    created_by TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX idx_script_apps_status ON script_apps(status);
CREATE INDEX idx_script_apps_manager ON script_apps(manager);
