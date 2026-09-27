# ADR-001: Go backend

Status: Accepted

## Decision

Use Go for the control-plane backend with `net/http` REST endpoints and explicit package boundaries.

## Rationale

Go produces a portable single binary, has strong concurrency and process/network primitives, and keeps provider contracts explicit. The standard HTTP router in Go 1.22+ supports method/path patterns without requiring a framework.

## Consequences

Domain packages own handlers/services/repositories instead of using a global controller. External dependencies are kept narrow and infrastructure-specific.
