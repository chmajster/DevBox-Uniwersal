/** Small, local SVG set: no icon font, network request or runtime dependency. */
const paths = {
  box: ['M12 3 3 7.5v9L12 21l9-4.5v-9L12 3Z', 'm3 7.5 9 4.5 9-4.5M12 12v9M7.5 5.25l9 4.5'],
  dashboard: ['M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z'],
  apps: ['M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z'],
  activity: ['M3 12h4l3-8 4 16 3-8h4'],
  database: ['M20 6c0 2-3.6 3-8 3S4 8 4 6s3.6-3 8-3 8 1 8 3Z', 'M4 6v12c0 2 3.6 3 8 3s8-1 8-3V6M4 12c0 2 3.6 3 8 3s8-1 8-3'],
  globe: ['M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18Z'],
  code: ['m8 6-6 6 6 6m8-12 6 6-6 6m-3-14-2 16'],
  network: ['M9 3h6v6H9zM3 15h6v6H3zM15 15h6v6h-6zM12 9v3M6 15v-3h12v3'],
  logs: ['M5 3h14v18H5zM8 7h8M8 11h8M8 15h5'],
  jobs: ['M8 5V3h8v2M5 5h14v16H5zM8 10l1 1 2-2m2 1h3m-8 6 1 1 2-2m2 1h3'],
  shield: ['m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z', 'm8 12 3 3 5-6'],
  backup: ['M3 11a9 9 0 1 1 2 7M3 5v6h6M12 7v5l3 2'],
  search: ['M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Zm-2 5 6 6'],
  chevron: ['m9 5 7 7-7 7'],
  arrow: ['M5 12h14m-6-6 6 6-6 6'],
  plus: ['M12 5v14M5 12h14'],
  refresh: ['M20 7v5h-5M4 17v-5h5M6 6a8 8 0 0 1 14 6M4 12a8 8 0 0 0 14 6'],
  moon: ['M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10Z'],
  sun: ['M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1'],
  logout: ['M9 4H4v16h5M10 12h11m-4-4 4 4-4 4'],
  menu: ['M4 6h16M4 12h16M4 18h16'],
  close: ['m6 6 12 12M6 18 18 6'],
  panel: ['M3 4h18v16H3zM9 4v16'],
  cpu: ['M7 7h10v10H7zM10 10h4v4h-4zM9 3v4m6-4v4M9 17v4m6-4v4M3 9h4m-4 6h4m10-6h4m-4 6h4'],
  memory: ['M3 6h18v12H3zM7 10h3v4H7zM14 10h3v4h-3zM7 18v3m5-3v3m5-3v3'],
  disk: ['M5 4h14l3 12v4H2v-4L5 4ZM2 16h20M6 18h1m3 0h1'],
  clock: ['M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM12 7v5l3 2'],
  list: ['M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01'],
  branch: ['M6 3v12m0-6c0 4 12 0 12 6M9 18a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm12 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z'],
  play: ['m8 4 12 8-12 8V4Z'],
  archive: ['M3 3h18v5H3zM5 8v13h14V8M10 12h4'],
  eye: ['M2 12s4-7 10-7 10 7 10 7-4 7-10 7-10-7-10-7Z', 'M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z'],
  lock: ['M5 10h14v11H5zM8 10V6a4 4 0 0 1 8 0v4M12 14v3'],
  check: ['m5 12 4 4L19 6'],
  alert: ['m12 3 10 18H2L12 3ZM12 9v5m0 3h.01']
} as const

export type IconName = keyof typeof paths

export function Icon({ name, size = 20, className = '' }: { name: IconName; size?: number; className?: string }) {
  return <svg className={`icon ${className}`} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
    {paths[name].map((d, index) => <path d={d} key={index} />)}
  </svg>
}
