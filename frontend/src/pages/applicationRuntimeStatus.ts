import type { Deployment, DockerContainer, Project } from '../api/types'
import { normalizeOperationalStatus } from '../status'
import { dockerContainersForProject } from './dockerContainerGroups'

function containerExitedWithFailure(container: DockerContainer) {
  if (container.state === 'dead') return true
  if (container.state !== 'exited') return false

  const match = container.status.match(/Exited\s+\((\d+)\)/i)
  return match ? Number(match[1]) !== 0 : false
}

export function resolveApplicationRuntimeStatus(
  project: Project,
  containers: DockerContainer[],
  dockerStateAvailable: boolean,
): string {
  if (!dockerStateAvailable) return project.status

  const projectContainers = dockerContainersForProject(containers, project)
  if (projectContainers.length === 0) {
    return normalizeOperationalStatus(project.status) === 'RUNNING' ? 'STOPPED' : project.status
  }

  if (projectContainers.some(containerExitedWithFailure)) return 'FAILED'

  const hasUnhealthyContainer = projectContainers.some((container) =>
    container.state === 'restarting' ||
    /\bunhealthy\b/i.test(container.status)
  )
  if (hasUnhealthyContainer) return 'UNHEALTHY'

  const runningCount = projectContainers.filter((container) => container.state === 'running').length
  if (runningCount === projectContainers.length) return 'RUNNING'
  if (runningCount > 0) return 'UNHEALTHY'

  return 'STOPPED'
}


export function resolveApplicationDisplayStatus(
  project: Project,
  containers: DockerContainer[],
  dockerStateAvailable: boolean,
  activeDeployment?: Deployment,
): string {
  if (activeDeployment) {
    const stage = activeDeployment.stage.trim().toUpperCase()
    const status = activeDeployment.status.trim().toUpperCase()
    const finished =
      status === 'SUCCESS' ||
      status === 'FAILED' ||
      stage === 'SUCCESS' ||
      stage === 'FAILED'

    if (!finished) {
      return stage === 'BUILDING' ? 'BUILDING' : 'DEPLOYING'
    }
  }

  return resolveApplicationRuntimeStatus(project, containers, dockerStateAvailable)
}
