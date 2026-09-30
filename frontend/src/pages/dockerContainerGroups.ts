import type { DockerContainer, Project } from '../api/types'

export type DockerContainerGroupKind = 'project' | 'infrastructure' | 'standalone'

export interface DockerContainerGroup {
  key: string
  label: string
  kind: DockerContainerGroupKind
  containers: DockerContainer[]
  running: number
  stopped: number
}

function normalizeProjectName(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '')
}

function localDirectoryName(path: string) {
  const normalized = path.replace(/\\/g, '/').replace(/\/+$/, '')
  const index = normalized.lastIndexOf('/')
  return index >= 0 ? normalized.slice(index + 1) : normalized
}

export function dockerContainerBelongsToProject(container: DockerContainer, project: Project) {
  const projectID = container.project_id?.trim()
  if (projectID) return projectID === project.id

  const composeProject = container.compose_project?.trim()
  if (!composeProject) return false

  const normalized = normalizeProjectName(composeProject)
  return project.slug === composeProject ||
    normalizeProjectName(project.slug) === normalized ||
    normalizeProjectName(project.name) === normalized ||
    normalizeProjectName(localDirectoryName(project.local_path)) === normalized
}

export function dockerContainersForProject(containers: DockerContainer[], project: Project) {
  return containers.filter((container) => dockerContainerBelongsToProject(container, project))
}

function projectForCompose(composeProject: string, projects: Project[]) {
  return projects.find((project) => dockerContainerBelongsToProject({
    id: '',
    name: '',
    image: '',
    state: '',
    status: '',
    compose_project: composeProject,
  }, project))
}

export function groupDockerContainers(containers: DockerContainer[], projects: Project[]): DockerContainerGroup[] {
  const projectsByID = new Map(projects.map((project) => [project.id, project]))
  const grouped = new Map<string, Omit<DockerContainerGroup, 'running' | 'stopped'>>()

  for (const container of containers) {
    let key: string
    let label: string
    let kind: DockerContainerGroupKind

    const projectID = container.project_id?.trim()
    const composeProject = container.compose_project?.trim()

    if (projectID) {
      const project = projectsByID.get(projectID)
      key = 'project:' + projectID
      label = project?.name || composeProject || 'Aplikacja ' + projectID.slice(0, 8)
      kind = 'project'
    } else if (composeProject) {
      const project = projectForCompose(composeProject, projects)
      key = project ? 'project:' + project.id : 'compose:' + composeProject
      label = project?.name || composeProject
      kind = 'project'
    } else if (container.name.startsWith('devbox-')) {
      key = 'infrastructure:devbox'
      label = 'DevBox / infrastruktura'
      kind = 'infrastructure'
    } else {
      key = 'standalone'
      label = 'Pozostałe kontenery'
      kind = 'standalone'
    }

    const current = grouped.get(key)
    if (current) {
      current.containers.push(container)
    } else {
      grouped.set(key, { key, label, kind, containers: [container] })
    }
  }

  const priority: Record<DockerContainerGroupKind, number> = {
    project: 0,
    infrastructure: 1,
    standalone: 2,
  }

  return [...grouped.values()]
    .map((group) => {
      const ordered = [...group.containers].sort((a, b) => a.name.localeCompare(b.name))
      const running = ordered.filter((container) => container.state === 'running').length
      return {
        ...group,
        containers: ordered,
        running,
        stopped: ordered.length - running,
      }
    })
    .sort((a, b) => priority[a.kind] - priority[b.kind] || a.label.localeCompare(b.label, 'pl'))
}
