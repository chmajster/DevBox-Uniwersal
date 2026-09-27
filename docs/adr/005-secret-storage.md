# ADR-005: Secret storage

Status: Accepted

## Decision

Persist only authenticated ciphertext. Use AES-256-GCM with a 32-byte master key supplied out-of-band. Store secret references in domain/provider configuration instead of plaintext credentials.

## Rationale

DevBox will manage Git, database, registry and system credentials. SQLite file permissions alone are insufficient protection against accidental disclosure or repository/log leakage.

## Consequences

No built-in default master key is generated and persisted silently. Key rotation/versioning is future work. Logs and API errors must never include secret material.
