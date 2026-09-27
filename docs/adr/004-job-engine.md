# ADR-004: Job Engine boundary

Status: Accepted

## Decision

External or long-running mutations use a Job Engine. `JobRunner` exposes registration, enqueue, cancel and retry. Durable job state and log tables are part of the initial schema.

## Rationale

Git operations, dependency installation, builds, Docker pulls and database provisioning can exceed HTTP request lifetimes and must survive UI navigation.

## Consequences

Agent 1 defines the contract and read API only. A later agent implements durable execution, recovery, cancellation semantics and log streaming.
