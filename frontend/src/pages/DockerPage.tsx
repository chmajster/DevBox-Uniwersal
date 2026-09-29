import { useEffect, useMemo, useState } from 'react'
import { request } from '../api/client'
import type { ComposeProcess, ComposeProject, DockerContainer, DockerImage, DockerNetwork, DockerStatus, DockerVolume, Project } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { groupDockerContainers } from './dockerContainerGroups'

type Section = 'containers' | 'images' | 'volumes' | 'networks' | 'compose'

export function DockerPage() {
  const { user } = useAuth()
  const [section, setSection] = useState<Section>('containers')
  const [status, setStatus] = useState<DockerStatus | null>(null)
  const [containers, setContainers] = useState<DockerContainer[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())
  const [images, setImages] = useState<DockerImage[]>([])
  const [volumes, setVolumes] = useState<DockerVolume[]>([])
  const [networks, setNetworks] = useState<DockerNetwork[]>([])
  const [composeProjects, setComposeProjects] = useState<ComposeProject[]>([])
  const [composeProcesses, setComposeProcesses] = useState<ComposeProcess[]>([])
  const [selectedCompose, setSelectedCompose] = useState('')
  const [logs, setLogs] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function refresh() {
    setError('')
    const dockerStatus = await request<DockerStatus>('/docker/status')
    setStatus(dockerStatus)
    if (!dockerStatus.available) {
      setContainers([])
      setProjects([])
      setImages([])
      setVolumes([])
      setNetworks([])
      setComposeProjects([])
      return
    }
    const [nextContainers, nextProjects, nextImages, nextVolumes, nextNetworks, nextCompose] = await Promise.all([
      request<DockerContainer[]>('/docker/containers'),
      request<Project[]>('/projects').catch(() => []),
      request<DockerImage[]>('/docker/images'),
      request<DockerVolume[]>('/docker/volumes'),
      request<DockerNetwork[]>('/docker/networks'),
      request<ComposeProject[]>('/docker/compose/projects')
    ])
    setContainers(nextContainers ?? [])
    setProjects(nextProjects ?? [])
    setImages(nextImages ?? [])
    setVolumes(nextVolumes ?? [])
    setNetworks(nextNetworks ?? [])
    setComposeProjects(nextCompose ?? [])
  }

  useEffect(() => {
    refresh().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Failed to load Docker data'))
  }, [])

  async function containerAction(id: string, action: 'start' | 'stop' | 'restart') {
    setBusy(true)
    setError('')
    try {
      await request<{ status: string }>('/docker/containers/' + encodeURIComponent(id) + '/' + action, { method: 'POST' })
      const next = await request<DockerContainer[]>('/docker/containers')
      setContainers(next ?? [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Docker operation failed')
    } finally {
      setBusy(false)
    }
  }

  async function showLogs(id: string) {
    setError('')
    try {
      const result = await request<{ logs: string }>('/docker/containers/' + encodeURIComponent(id) + '/logs?tail=300')
      setLogs(result.logs)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load logs')
    }
  }

  async function showCompose(project: string) {
    setError('')
    try {
      const items = await request<ComposeProcess[]>('/docker/compose/projects/' + encodeURIComponent(project) + '/ps')
      setSelectedCompose(project)
      setComposeProcesses(items ?? [])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load Compose state')
    }
  }

  const canOperate = user?.role === 'admin' || user?.role === 'operator'

  const containerGroups = useMemo(() => groupDockerContainers(containers, projects), [containers, projects])

  function toggleContainerGroup(key: string) {
    setCollapsedGroups((current) => {
      const next = new Set(current)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return <>
    <div className="page-heading">
      <div>
        <h1>Docker</h1>
        <p className="muted">Live state is read from the local Docker engine.</p>
      </div>
      <button type="button" className="secondary-button" onClick={() => refresh().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Failed to refresh'))}>Refresh</button>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {status && <div className={'docker-status ' + (status.available ? 'is-up' : 'is-down')}>
      <strong>{status.available ? 'Docker available' : 'Docker unavailable'}</strong>
      <span>{status.server_version || status.client_version || status.error || 'No Docker engine detected'}</span>
      {status.available && <span>{status.containers_running} running / {status.containers} containers · {status.images} images</span>}
    </div>}

    <div className="tabs" role="tablist" aria-label="Docker sections">
      {([
        ['containers', 'Containers'],
        ['images', 'Images'],
        ['volumes', 'Volumes'],
        ['networks', 'Networks'],
        ['compose', 'Compose Projects']
      ] as Array<[Section, string]>).map(([key, label]) =>
        <button key={key} type="button" className={section === key ? 'active' : ''} onClick={() => setSection(key)}>{label}</button>
      )}
    </div>

    {status?.available === false && <div className="empty-panel">Docker data is unavailable because the engine cannot be queried.</div>}

    {status?.available && section === 'containers' && <div className="docker-container-groups">
      {containerGroups.map((group) => {
        const collapsed = collapsedGroups.has(group.key)
        const groupDescription = group.kind === 'project'
          ? 'Aplikacja / projekt'
          : group.kind === 'infrastructure'
            ? 'Usługi infrastrukturalne DevBox'
            : 'Kontenery bez przypisanego projektu'
        return <section className={'docker-container-card docker-container-card-' + group.kind} key={group.key}>
          <button
            type="button"
            className="docker-container-card-header"
            aria-expanded={!collapsed}
            onClick={() => toggleContainerGroup(group.key)}
          >
            <span className="docker-container-card-chevron" aria-hidden="true">{collapsed ? '›' : '⌄'}</span>
            <span className="docker-container-card-title">
              <strong>{group.label}</strong>
              <small>{groupDescription} · {group.containers.length} {group.containers.length === 1 ? 'kontener' : 'kontenery'}</small>
            </span>
            <span className="docker-container-card-summary">
              <span className="state-pill state-running">{group.running} running</span>
              {group.stopped > 0 && <span className="state-pill state-exited">{group.stopped} stopped</span>}
            </span>
          </button>
          {!collapsed && <div className="docker-container-card-body">
            <div className="table-wrap docker-container-table-wrap"><table>
              <thead><tr><th>Name</th><th>Image</th><th>State</th><th>Status</th><th>Ports</th><th>Actions</th></tr></thead>
              <tbody>
                {group.containers.map((item) => <tr key={item.id}>
                  <td><strong>{item.name}</strong><div className="mono muted">{item.id.slice(0, 12)}</div></td>
                  <td>{item.image}</td>
                  <td><span className={'state-pill state-' + item.state}>{item.state}</span></td>
                  <td>{item.status}</td>
                  <td className="mono">{item.ports || '—'}</td>
                  <td><div className="row-actions">
                    <button type="button" className="secondary-button" onClick={() => showLogs(item.id)}>Logs</button>
                    {canOperate && item.state !== 'running' && <button type="button" disabled={busy} onClick={() => containerAction(item.id, 'start')}>Start</button>}
                    {canOperate && item.state === 'running' && <button type="button" disabled={busy} onClick={() => containerAction(item.id, 'stop')}>Stop</button>}
                    {canOperate && <button type="button" className="secondary-button" disabled={busy} onClick={() => containerAction(item.id, 'restart')}>Restart</button>}
                  </div></td>
                </tr>)}
              </tbody>
            </table></div>
          </div>}
        </section>
      })}
      {containerGroups.length === 0 && <div className="empty-panel">No containers reported by Docker.</div>}
    </div>}

    {status?.available && section === 'images' && <div className="table-wrap"><table>
      <thead><tr><th>Repository</th><th>Tag</th><th>ID</th><th>Size</th><th>Created</th></tr></thead>
      <tbody>
        {images.map((item) => <tr key={item.id + ':' + item.repository + ':' + item.tag}><td>{item.repository}</td><td>{item.tag}</td><td className="mono">{item.id.slice(0, 20)}</td><td>{item.size}</td><td>{item.created_since}</td></tr>)}
        {images.length === 0 && <tr><td colSpan={5} className="muted">No images reported by Docker.</td></tr>}
      </tbody>
    </table></div>}

    {status?.available && section === 'volumes' && <div className="table-wrap"><table>
      <thead><tr><th>Name</th><th>Driver</th><th>Scope</th><th>Mountpoint</th></tr></thead>
      <tbody>
        {volumes.map((item) => <tr key={item.name}><td>{item.name}</td><td>{item.driver}</td><td>{item.scope || '—'}</td><td className="mono">{item.mountpoint || '—'}</td></tr>)}
        {volumes.length === 0 && <tr><td colSpan={4} className="muted">No volumes reported by Docker.</td></tr>}
      </tbody>
    </table></div>}

    {status?.available && section === 'networks' && <div className="table-wrap"><table>
      <thead><tr><th>Name</th><th>ID</th><th>Driver</th><th>Scope</th><th>Internal</th><th>IPv6</th></tr></thead>
      <tbody>
        {networks.map((item) => <tr key={item.id}><td>{item.name}</td><td className="mono">{item.id.slice(0, 16)}</td><td>{item.driver}</td><td>{item.scope || '—'}</td><td>{item.internal || 'false'}</td><td>{item.ipv6 || 'false'}</td></tr>)}
        {networks.length === 0 && <tr><td colSpan={6} className="muted">No networks reported by Docker.</td></tr>}
      </tbody>
    </table></div>}

    {status?.available && section === 'compose' && <div className="split-panel">
      <div className="table-wrap"><table>
        <thead><tr><th>Project</th><th>Compose file</th><th>Docker state</th></tr></thead>
        <tbody>
          {composeProjects.map((item) => <tr key={item.name}><td>{item.name}</td><td>{item.config_file}</td><td><button type="button" className="secondary-button" onClick={() => showCompose(item.name)}>Load ps</button></td></tr>)}
          {composeProjects.length === 0 && <tr><td colSpan={3} className="muted">No supported Compose files found under the configured projects root.</td></tr>}
        </tbody>
      </table></div>
      {selectedCompose && <div className="detail-panel">
        <h2>{selectedCompose}</h2>
        {composeProcesses.map((item) => <div className="compose-process" key={item.name || item.service}><strong>{item.service || item.name}</strong><span>{item.state || 'unknown'}{item.health ? ' · ' + item.health : ''}</span><span className="muted">{item.image}</span></div>)}
        {composeProcesses.length === 0 && <p className="muted">Docker Compose reports no containers for this project.</p>}
      </div>}
    </div>}

    {logs && <section className="logs-panel"><div className="logs-heading"><h2>Container logs</h2><button type="button" className="secondary-button" onClick={() => setLogs('')}>Close</button></div><pre>{logs}</pre></section>}
  </>
}
