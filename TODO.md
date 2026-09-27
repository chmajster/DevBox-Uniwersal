# TODO

Items intentionally left outside Agent 1 scope:

- Implement Git provider and Project service/repository/HTTP module.
- Implement durable JobRunner and worker; current work defines contracts and read API only.
- Implement runtime registry and concrete runtimes.
- Implement DockerProvider.
- Implement MySQL DatabaseProvider.
- Implement Nginx ReverseProxyProvider.
- Implement SQLite-backed PortAllocator with OS socket reconciliation.
- Implement Windows/WSL system discovery and privilege-separated helper/service.
- Implement monitoring scheduler and health history.
- Add CSRF protection before introducing browser state-changing domain endpoints beyond auth.
- Add login rate limiting and session administration.
- Add secret-key rotation/versioning and wire SecretStore into concrete provider credential flows.
- Add Project/Deployment domain UI after their backend modules exist.
- Add end-to-end browser tests once domain workflows are available.
