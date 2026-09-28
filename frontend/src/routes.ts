export const ROUTES = {
  dashboard: '/',
  applications: '/apps',
  project: '/projects/:projectId/:tab?',
  logs: '/logs',
  health: '/health',
  backups: '/backups',
  jobs: '/jobs',
  audit: '/audit'
} as const

export const PROJECT_TABS = [
  'overview',
  'configuration',
  'runtime',
  'git',
  'deployments',
  'logs',
  'environment',
  'database',
  'networking',
  'backups'
] as const

export type ProjectTab = typeof PROJECT_TABS[number]
