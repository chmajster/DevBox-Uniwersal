import { useParams } from 'react-router-dom'
import { ProjectRuntimeSection } from '../runtime/ProjectRuntimeSection'

export function ProjectRuntimePage() {
  const { projectId } = useParams()
  if (!projectId) {
    return <div className="error-banner">Project ID is required.</div>
  }
  return <ProjectRuntimeSection projectId={projectId} />
}
