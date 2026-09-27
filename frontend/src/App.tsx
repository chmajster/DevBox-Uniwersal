import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { AppLayout } from './layout/AppLayout'
import { AuditPage } from './pages/AuditPage'
import { DashboardPage } from './pages/DashboardPage'
import { DatabasesPage } from './pages/DatabasesPage'
import { DockerPage } from './pages/DockerPage'
import { DomainsPage } from './pages/DomainsPage'
import { JobsPage } from './pages/JobsPage'
import { LoginPage } from './pages/LoginPage'
import { PortsPage } from './pages/PortsPage'
import { ProjectRuntimePage } from './pages/ProjectRuntimePage'
import { RuntimeManagerPage } from './pages/RuntimeManagerPage'

export function App() {
  return <BrowserRouter><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path="databases" element={<DatabasesPage />} />
        <Route path="domains" element={<DomainsPage />} />
        <Route path="ports" element={<PortsPage />} />
        <Route path="docker" element={<DockerPage />} />
        <Route path="runtimes" element={<RuntimeManagerPage />} />
        <Route path="projects/:projectId/runtime" element={<ProjectRuntimePage />} />
        <Route path="jobs" element={<JobsPage />} />
        <Route path="audit" element={<AuditPage />} />
      </Route>
    </Route>
  </Routes></BrowserRouter>
}
