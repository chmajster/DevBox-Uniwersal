import { useEffect, useMemo, useState } from 'react'
import {
  getMonitoringSnapshot,
  getServiceProbe,
  listDatabases,
  listDockerContainers,
  listPorts,
  listProjects
} from '../api/operations'
import type { MonitoringSnapshot, Project, ServiceProbe } from '../api/types'
import { ErrorState } from '../components/ErrorState'
import { MetricCard } from '../components/MetricCard'
import { StatusBadge } from '../components/StatusBadge'
import { normalizeOperationalStatus } from '../status'

interface DashboardState {
  monitoring: MonitoringSnapshot | null
  projects: Project[]
  dockerContainers: number | null
  databases: number | null
  ports: number | null
  services: ServiceProbe[]
  errors: string[]
}

const initialState: DashboardState = {
  monitoring: null,
  projects: [],
  dockerContainers: null,
  databases: null,
  ports: null,
  services: [],
  errors: []
}

function reasonMessage(reason: unknown) {
  return reason instanceof Error ? reason.message : String(reason)
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / 1024 ** index).toFixed(index >= 3 ? 1 : 0)} ${units[index]}`
}

function formatUptime(seconds: number) {
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return days > 0 ? `${days}d ${hours}h` : `${hours}h ${minutes}m`
}

export function DashboardPage() {
  const [state, setState] = useState<DashboardState>(initialState)

  useEffect(() => {
    let cancelled = false
    async function load() {
      const [monitoring, projects, containers, databases, ports, nginx, mysql, docker, devbox] = await Promise.allSettled([
        getMonitoringSnapshot(),
        listProjects(),
        listDockerContainers(),
        listDatabases(),
        listPorts(),
        getServiceProbe('Nginx'),
        getServiceProbe('MySQL'),
        getServiceProbe('Docker'),
        getServiceProbe('DevBox')
      ])
      if (cancelled) return

      const errors: string[] = []
      const serviceResults: ServiceProbe[] = []
      for (const [name, result] of [
        ['Nginx', nginx],
        ['MySQL', mysql],
        ['Docker', docker],
        ['DevBox', devbox]
      ] as const) {
        if (result.status === 'fulfilled') {
          serviceResults.push(result.value)
        } else {
          const message = reasonMessage(result.reason)
          serviceResults.push({ name, status: 'UNHEALTHY', message })
          errors.push(`${name}: ${message}`)
        }
      }
      if (monitoring.status === 'rejected') errors.push(`Monitoring: ${reasonMessage(monitoring.reason)}`)
      if (projects.status === 'rejected') errors.push(`Applications: ${reasonMessage(projects.reason)}`)
      if (containers.status === 'rejected') errors.push(`Docker containers: ${reasonMessage(containers.reason)}`)
      if (databases.status === 'rejected') errors.push(`Databases: ${reasonMessage(databases.reason)}`)
      if (ports.status === 'rejected') errors.push(`Ports: ${reasonMessage(ports.reason)}`)

      setState({
        monitoring: monitoring.status === 'fulfilled' ? monitoring.value : null,
        projects: projects.status === 'fulfilled' ? projects.value : [],
        dockerContainers: containers.status === 'fulfilled' ? containers.value.length : null,
        databases: databases.status === 'fulfilled' ? databases.value.length : null,
        ports: ports.status === 'fulfilled' ? ports.value.length : null,
        services: serviceResults,
        errors
      })
    }
    void load()
    return () => { cancelled = true }
  }, [])

  const counts = useMemo(() => {
    const normalized = state.projects.map((project) => normalizeOperationalStatus(project.status))
    return {
      applications: state.projects.length,
      running: normalized.filter((status) => status === 'RUNNING').length,
      stopped: normalized.filter((status) => status === 'STOPPED').length,
      failed: normalized.filter((status) => status === 'FAILED' || status === 'UNHEALTHY').length
    }
  }, [state.projects])

  const monitoring = state.monitoring

  return <>
    <div className="page-heading">
      <div>
        <h1>Dashboard</h1>
        <p className="muted">Operational state of DevBox and managed application resources.</p>
      </div>
    </div>

    <div className="metric-grid">
      <MetricCard label="Applications" value={String(counts.applications)} />
      <MetricCard label="Running" value={String(counts.running)} />
      <MetricCard label="Stopped" value={String(counts.stopped)} />
      <MetricCard label="Failed" value={String(counts.failed)} />
      <MetricCard label="Docker containers" value={state.dockerContainers === null ? '—' : String(state.dockerContainers)} />
      <MetricCard label="Databases" value={state.databases === null ? '—' : String(state.databases)} />
      <MetricCard label="Ports" value={state.ports === null ? '—' : String(state.ports)} />
      <MetricCard
        label="CPU"
        value={monitoring?.cpu.available ? `${monitoring.cpu.usage_percent.toFixed(1)}%` : 'Unavailable'}
        percent={monitoring?.cpu.available ? monitoring.cpu.usage_percent : undefined}
        detail={monitoring ? `${monitoring.cpu.cores} cores` : undefined}
      />
      <MetricCard
        label="RAM"
        value={monitoring?.memory.available ? `${monitoring.memory.usage_percent.toFixed(1)}%` : 'Unavailable'}
        percent={monitoring?.memory.available ? monitoring.memory.usage_percent : undefined}
        detail={monitoring?.memory.available ? `${formatBytes(monitoring.memory.used_bytes)} / ${formatBytes(monitoring.memory.total_bytes)}` : undefined}
      />
      <MetricCard
        label="Disk"
        value={monitoring?.disk.available ? `${monitoring.disk.usage_percent.toFixed(1)}%` : 'Unavailable'}
        percent={monitoring?.disk.available ? monitoring.disk.usage_percent : undefined}
        detail={monitoring?.disk.available ? `${formatBytes(monitoring.disk.used_bytes)} / ${formatBytes(monitoring.disk.total_bytes)}` : undefined}
      />
      <MetricCard
        label="Uptime"
        value={monitoring ? formatUptime(monitoring.host_uptime_seconds) : '—'}
        detail={monitoring ? `${monitoring.process.host_process_count} host processes` : undefined}
      />
    </div>

    <h2>Services</h2>
    <div className="service-grid">
      {state.services.map((service) => (
        <article className="service-card" key={service.name}>
          <div>
            <span>{service.name}</span>
            {service.message && <small>{service.message}</small>}
          </div>
          <StatusBadge status={service.status} />
        </article>
      ))}
    </div>

    {monitoring?.warnings?.map((warning) => <ErrorState key={warning} message={warning} />)}
    {state.errors.length > 0 && <section className="integration-errors">
      <h2>Integration errors</h2>
      {state.errors.map((error) => <ErrorState key={error} message={error} />)}
    </section>}
  </>
}
