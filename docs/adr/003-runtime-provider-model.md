# ADR-003: Runtime and provider model

Status: Accepted

## Decision

Separate application runtimes from infrastructure providers. `runtimes.Runtime` owns application lifecycle semantics. `providers.*` interfaces own Git, process, Docker, database, reverse-proxy, port and system-service capabilities.

## Rationale

A Python runtime may run directly, under a process manager, or inside Docker. A database provider is not a runtime. Separating these axes avoids package coupling and allows agents to work independently.

## Consequences

Provider-specific options stay in module-owned configuration. Shared contracts change only for capabilities required by multiple modules.
