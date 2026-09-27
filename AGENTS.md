# AGENTS.md

## Required reading

Before changing code, every agent must read:

1. `AGENTS.md`
2. `ARCHITECTURE.md`
3. `STATUS.md`
4. `CHANGELOG.md`
5. all ADRs in `docs/adr/`

Read `ROADMAP.md` and `TODO.md` for scope and ownership context.

## Branching

Every agent works on its own branch. Never develop directly on `main`. Branch names should follow `agent/<number>-<scope>` where practical.

## Module ownership

Do not change another agent's module unless required by an explicit contract change or integration fix. Keep Git/Projects, runtimes, Docker, databases, proxy, monitoring, frontend and Windows/WSL implementation work isolated in their packages.

Do not create shared files that become dumping grounds. In particular, do not create a monolithic `api/controllers.go`. A domain that needs HTTP and persistence should normally own its handler, service, repository and models.

Cross-module behavior must use interfaces from `backend/internal/runtimes`, `backend/internal/jobs`, `backend/internal/secrets` and `backend/internal/providers`. If a shared contract must change, document the reason in an ADR or update the relevant ADR before broad edits.

## Implementation rules

- Mocks, fake providers and placeholder endpoints are not acceptable as final implementations.
- Do not log passwords, session tokens, private keys, connection strings containing credentials, secret plaintext or decrypted secret material.
- Never persist secret plaintext. Use `secrets.SecretStore` and encrypted storage.
- All mutating operations that may take time or touch external systems should be designed for the Job Engine rather than blocking HTTP handlers.
- API additions belong under `/api/v1` unless a future version is deliberately introduced.
- Use the common API success/error envelope.
- Preserve RBAC and auditability for privileged operations.
- Database schema changes require an additive ordered migration. Never rewrite a migration already released to `main`.
- Provider packages must not reach into another provider's concrete implementation.
- Avoid package-level global mutable state.
- Keep public contract types small and serializable where possible.

## Before opening a PR

Run backend formatting, vetting, tests and build. Run frontend install, lint, typecheck, tests and build. Update `STATUS.md` and `CHANGELOG.md`. List any contract changes and migration changes in the PR description.
