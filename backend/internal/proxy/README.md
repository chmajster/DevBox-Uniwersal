# Proxy and SSL

Nginx routes terminate HTTP/TLS for published application endpoints. Application routing reserves hostname ownership, applies validated candidates and reloads only after global validation. Existing domain certificates live under `<database directory>/certificates/<hostname>/{fullchain.pem,privkey.pem}`. ACME is not configured.

The same module lists durable application/infrastructure port reservations and domain route activity. Application port mutations are made through application settings/jobs; deleting an application releases its reservations and routes while preserving source/data volumes. See [Application hosting](../../../docs/application-control-plane.md).
