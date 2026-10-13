# Docker module

This module implements controlled `docker` and `docker compose` CLI operations. HTTP handlers delegate application work to the Application Service and jobs; they cannot supply raw CLI arguments. Processes are invoked with argument arrays, without a host shell.

Application execution has two drivers: Existing Compose and DevBox Managed Runtime. Both use this provider for isolated, labelled Docker resources, observed status, published ports and separate stdout/stderr logs. Managed runtimes build images from the central catalog and bind mount the original source read/write. Generated runtime configuration is saved outside source under `<DEVBOX_DATA>/projects/<application-id>/runtime/`.

Existing Compose keeps the user's files unchanged. Private overrides supply application ownership labels, names, runtime environment, the selected command and persistent port mappings. Non-external networks and volumes are scoped to the immutable application ID. External resources and named volume data survive ordinary removal.

Application paths are validated against configured browsing/source roots. Inventory and infrastructure diagnostics remain separate from application lifecycle. Secret values are supplied through temporary private files, excluded from saved runtime artifacts, and redacted from application/job output. Inspection and normalized configuration output are never streamed to job logs.

See [Application control plane](../../../docs/application-control-plane.md) for lifecycle, permissions, Compose prerequisites and the verification matrix.
