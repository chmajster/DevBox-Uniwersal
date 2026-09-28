import type { Project } from '../api/types'
import { normalizeOperationalStatus } from '../status'

export const projectStatuses = ['RUNNING', 'STOPPED', 'BUILDING', 'DEPLOYING', 'FAILED', 'UNHEALTHY'] as const
export function filterProjects(projects: Project[], query: string, status: string): Project[] {
  const search = query.trim().toLocaleLowerCase('pl')
  return projects.filter((project) => {
    const matchesStatus = !status || normalizeOperationalStatus(project.status) === status
    const text = [project.name, project.description, project.runtime, project.branch, project.domain].filter(Boolean).join(' ').toLocaleLowerCase('pl')
    return matchesStatus && (!search || text.includes(search))
  })
}
