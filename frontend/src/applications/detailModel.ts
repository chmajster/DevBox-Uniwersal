import type { IconName } from '../components/Icon'
import type { APPLICATION_TABS } from '../routes'
import type { ApplicationDetail } from './model'

export type ApplicationTab = typeof APPLICATION_TABS[number]
export const tabLabels: Record<ApplicationTab, { label: string; icon: IconName }> = {
  Overview: { label: 'Przegląd', icon: 'dashboard' },
  Runtime: { label: 'Środowisko', icon: 'code' },
  Database: { label: 'Baza danych', icon: 'database' },
  Environment: { label: 'Zmienne i sekrety', icon: 'lock' },
  Ports: { label: 'Porty', icon: 'network' },
  Domains: { label: 'Domeny', icon: 'globe' },
  Logs: { label: 'Logi', icon: 'logs' },
  Jobs: { label: 'Zadania', icon: 'jobs' },
  Docker: { label: 'Kontenery', icon: 'box' },
  Settings: { label: 'Ustawienia', icon: 'panel' },
  Services: { label: 'Usługi', icon: 'list' },
}
export const runtimeNames: Record<string, string> = { php: 'PHP', node: 'Node.js', python: 'Python', go: 'Go', static: 'Static', wordpress: 'WordPress', dockerfile: 'Dockerfile', image: 'Obraz OCI' }
export function runtimeLabel(app: ApplicationDetail) {
  if (!app.runtime) return 'Nie wdrożono'
  return `${runtimeNames[app.runtime.name] ?? app.runtime.name} ${app.runtime.version ?? ''}`.trim()
}
export function activeRuntimeMetadata(app: ApplicationDetail): Record<string, unknown> {
  // A newer failed plan does not describe the container still serving traffic.
  const deployed = app.deployments.find((item) => item.status === 'success' && item.driver === app.driver)
  return { ...deployed?.plan_snapshot?.runtime?.metadata, ...app.runtime?.metadata }
}
