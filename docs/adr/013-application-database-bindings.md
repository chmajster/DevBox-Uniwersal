# ADR-013: Application-owned managed database bindings

Status: Accepted.

## Context

The application control plane is independent of the legacy Projects module, but the existing database binding table is keyed to project IDs. WordPress Auto Containers need a safe way to select a database already managed by DevBox and receive its connection details without exposing its password through the API or duplicating the database account store.

## Decision

Add an application-owned binding relation that references the existing `databases` resource. It stores only the application and database identifiers. Database accounts, grants, and encrypted credentials remain owned by the Databases module.

The application binding API supports managed MySQL and MariaDB databases. It selects an account already granted access to the chosen database. Deployment resolves its Docker application endpoint and SecretStore credential through the provider contract, then injects the WordPress database environment as sensitive container configuration. Missing bindings leave WordPress deployments in `waiting_for_configuration`.

Application runtime metadata also carries generated-image identity, source and container paths, network, runtime type, and volume inventory so the existing `/apps` detail view can report the active execution environment.

Migration `016_application_database_bindings.sql` adds the relation without changing project binding tables or released migrations.

## Consequences

- `/apps` can use existing managed SQL databases without depending on Project records.
- Password plaintext remains in SecretStore and is never returned by binding reads.
- WordPress binding is limited to MySQL/MariaDB; Compose-owned and external database binding for Applications remain future work.
- Removing an application removes its binding relation while preserving the database itself.
