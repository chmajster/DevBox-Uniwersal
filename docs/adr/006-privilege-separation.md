# ADR-006: Privilege separation

Status: Accepted

## Decision

Run the web/API process unprivileged. Operations requiring Administrator/root privileges must use a narrow `SystemServiceProvider` or future privileged helper with an explicit command surface and audit trail.

## Rationale

Running a network-facing application manager permanently elevated would turn application-layer compromise into full host compromise.

## Consequences

Windows/WSL and Linux service-management agents must design explicit privileged operations rather than adding generic shell-as-root endpoints.
