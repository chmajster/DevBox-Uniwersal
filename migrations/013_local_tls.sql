CREATE TABLE domain_certificates (
    domain_id TEXT PRIMARY KEY REFERENCES domains(id) ON DELETE CASCADE,
    hostname TEXT NOT NULL,
    certificate_pem TEXT NOT NULL,
    key_secret_name TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    not_after TEXT NOT NULL,
    verified_at TEXT NOT NULL
);
