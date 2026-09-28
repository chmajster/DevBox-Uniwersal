import type { Role } from '../api/types'
import type { IconName } from '../components/Icon'
import { ROUTES } from '../routes'
export interface NavigationItem { to: string; label: string; icon: IconName; group: string; roles?: Role[] }
const items: NavigationItem[] = [
  { to: ROUTES.dashboard, label: 'Przegląd', icon: 'dashboard', group: 'Główne' },
  { to: ROUTES.applications, label: 'Aplikacje', icon: 'apps', group: 'Główne' },
  { to: '/docker', label: 'Kontenery', icon: 'box', group: 'Główne' },
  { to: '/databases', label: 'Bazy danych', icon: 'database', group: 'Główne' },
  { to: '/domains', label: 'Domeny i proxy', icon: 'globe', group: 'Główne' },
  { to: '/ports', label: 'Porty', icon: 'network', group: 'Główne' },
  { to: ROUTES.plugins, label: 'Pluginy', icon: 'puzzle', group: 'Infrastruktura' },
  { to: ROUTES.jobs, label: 'Zadania', icon: 'jobs', group: 'Operacje' },
  { to: ROUTES.logs, label: 'Logi', icon: 'logs', group: 'Operacje' },
  { to: ROUTES.health, label: 'Monitoring', icon: 'activity', group: 'Operacje' },
  { to: ROUTES.backups, label: 'Kopie zapasowe', icon: 'backup', group: 'Operacje', roles: ['admin'] },
  { to: ROUTES.audit, label: 'Audyt', icon: 'shield', group: 'Operacje', roles: ['admin', 'operator'] },
  { to: '/runtimes', label: 'Runtime', icon: 'code', group: 'Infrastruktura' }
]
export function navigationForRole(role?: Role): NavigationItem[] { return items.filter((item) => !item.roles || (role !== undefined && item.roles.includes(role))) }
export function navigationForPath(pathname: string, role?: Role): NavigationItem | undefined {
  const path = pathname.startsWith('/projects/') ? ROUTES.applications : pathname
  return navigationForRole(role).find((item) => item.to === '/' ? path === '/' : path === item.to || path.startsWith(`${item.to}/`))
}
export function readPreference(key: string, fallback: string): string {
  try { return typeof window === 'undefined' ? fallback : window.localStorage.getItem(key) ?? fallback } catch { return fallback }
}
export function savePreference(key: string, value: string): void {
  try { window.localStorage.setItem(key, value) } catch { /* Browser storage is optional. */ }
}
