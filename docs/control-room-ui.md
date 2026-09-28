# Selected control-room UI

The selected dark operator-console design is implemented in the existing React/Vite SPA. It replaces the earlier workspace dashboard, rather than creating a mock page. Dark is the initial theme; explicit light/dark choices are stored under `devbox-control-theme`. The previous workspace preference does not override the newly selected design.

## Layout and navigation

Compact grouped sidebar, command search for authorized views and loaded applications, host/version information, component-health summary, application cards/tables and mobile navigation retain the existing routes. The screenshot's duplicate Docker/queue entries and unsupported storage/network pages are not invented. Runtime remains a separate infrastructure view. All existing module operations and authorization boundaries remain.

## Data contracts

| Panel | Existing endpoints | Refresh |
| --- | --- | --- |
| Inventory, service state and host summary | `/projects`, `/docker/containers`, `/databases`, `/ports`, `/jobs`, `/system/info`, `/health`, `/docker/status`, `/proxy/status`, `/mysql/status` | 30 seconds after the previous read |
| CPU/RAM/disk gauges and chart | `/monitoring/snapshot` | 10 seconds while dashboard is mounted |
| Log preview | `/logs/sources`, `/logs?source=...&limit=...` | 5 seconds; 20/50/100 lines |
| Job detail and full logs | existing job/log APIs and SSE | existing workflows |

Requests retain the shared API envelope, authenticated cookies and CSRF handling. Polling is serialized, has a 15-second timeout, aborts on cleanup/manual refresh and pauses while the browser tab is hidden. It does not create worker jobs or issue infrastructure mutations.

An unavailable collection renders a dash, not a fabricated zero. Service read failures are UNKNOWN, not RUNNING. The header health indicator concerns the four probed components, not a promise that every managed project is healthy. Readings older than 75 seconds are marked unavailable in the header. Service timing is browser-to-API request elapsed time; it is not an instrumented service response latency. Nginx health uses the existing detection/config-validation contract.

## Chart semantics and limitations

Chart points come only from successfully read snapshot timestamps. In-memory session history is limited to two hours and 721 samples. Duplicate/out-of-order timestamps are rejected, missing values create gaps, and gaps over 30 seconds are not connected. The time axis fits the available portion of the selected 10-minute/30-minute/2-hour window. A new session starts with an empty/single-point chart; it never fabricates historical utilization.

The current API does not expose CPU temperature or host-network byte counters. Temperature therefore displays `Brak danych`; there is no pretend network chart. Version, hostname, counts, port allocations and free disk space are read from the actual API. A database inventory count does not claim every database is running.

The dashboard job counter includes queued/pending/running jobs. Its link opens `/jobs?status=active`; a job ID opens `/jobs?job=...`. Completed jobs have a completion badge, never a RUNNING badge. Log preview source selection carries into `/logs?source=...`.

## Validation

`npm run lint`, `npm run typecheck`, `npm test` and `npm run build` remain the frontend gates. `src/control-room/model.test.ts` covers metric bounds, missing data, retention, timestamp ordering, gaps, collection normalization and job semantics. The `Workspace UI` workflow executes `e2e/workspace_smoke.py` against the production bundle in Chromium, with explicitly synthetic API fixtures. It exercises new panels, charts, filters, deep links, role visibility, blocked storage, login, CSRF-preserving project actions and 900/390/320-pixel layouts, and uploads screenshots plus a report.

These browser fixtures are test-only and are not shipped in the runtime bundle. Browser smoke is not a real Docker/MySQL/Nginx deployment test.
