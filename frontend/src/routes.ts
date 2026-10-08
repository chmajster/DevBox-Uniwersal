export const ROUTES = {
  dashboard: '/',
  applications: '/apps',
  credentials: '/credentials',
  users: '/users',
  databaseUsers: '/database-users',
  application: '/apps/:id',
  logs: '/logs',
  health: '/health',
  backups: '/backups',
  plugins: '/plugins',
  updates: '/updates',
  jobs: '/jobs',
  audit: '/audit'
} as const

export const APPLICATION_TABS = ['Overview', 'Runtime', 'Environment', 'Ports', 'Domains', 'Logs', 'Jobs', 'Docker', 'Settings', 'Services'] as const
