# ADR-002: SQLite bootstrap database

Status: Accepted

## Decision

Use SQLite as the initial durable control-plane store. Enable foreign keys, WAL and a busy timeout. Evolve schema through ordered immutable SQL migrations.

## Rationale

DevBox Universal targets a local single-host control plane. SQLite avoids requiring another service before DevBox can manage services such as MySQL.

## Consequences

The database connection is serialized initially. Repository contracts must prevent SQL details from leaking into services so a future database change remains possible.
