import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { AppLayout } from './layout/AppLayout'
import { AuditPage } from './pages/AuditPage'
import { BackupsPage } from './pages/BackupsPage'
import { DashboardPage } from './pages/DashboardPage'
import { DatabasesPage } from './pages/DatabasesPage'
import { CredentialsPage } from './pages/CredentialsPage'
import { DockerPage } from './pages/DockerPage'
import { DomainsPage } from './pages/DomainsPage'
import { JobsPage } from './pages/JobsPage'
import { HealthPage } from './pages/HealthPage'
import { LogsPage } from './pages/LogsPage'
import { LoginPage } from './pages/LoginPage'
import { PortsPage } from './pages/PortsPage'
import { PluginsPage } from './pages/PluginsPage'
import { ProjectDetailPage } from './pages/ProjectDetailPage'
import { ProjectDetailsPage } from './pages/ProjectDetailsPage'
import { ProjectWizardPage } from './pages/ProjectWizardPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { ROUTES } from './routes'
import { ProjectRuntimePage } from './pages/ProjectRuntimePage'
import { RuntimeManagerPage } from './pages/RuntimeManagerPage'
import { ScriptAppsPage } from './pages/ScriptAppsPage'

export function App() {
  return <BrowserRouter><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path={ROUTES.applications} element={<ProjectsPage />} />
        <Route path={ROUTES.credentials} element={<CredentialsPage />} />
        <Route path={ROUTES.scriptApps} element={<ScriptAppsPage />} />
        <Route path="/applications" element={<Navigate to={ROUTES.applications} replace />} />
        <Route path="apps/new" element={<ProjectWizardPage />} />
        <Route path="apps/:id" element={<ProjectDetailPage />} />
        <Route path={ROUTES.project} element={<ProjectDetailsPage />} />
        <Route path={ROUTES.logs} element={<LogsPage />} />
        <Route path={ROUTES.health} element={<HealthPage />} />
        <Route path={ROUTES.backups} element={<BackupsPage />} />
        <Route path="databases" element={<DatabasesPage />} />
        <Route path="domains" element={<DomainsPage />} />
        <Route path="ports" element={<PortsPage />} />
        <Route path={ROUTES.plugins} element={<PluginsPage />} />
        <Route path="docker" element={<DockerPage />} />
        <Route path="runtimes" element={<RuntimeManagerPage />} />
        <Route path="projects/:projectId/runtime" element={<ProjectRuntimePage />} />
        <Route path={ROUTES.jobs} element={<JobsPage />} />
        <Route path={ROUTES.audit} element={<AuditPage />} />
      </Route>
    </Route>
  </Routes></BrowserRouter>
}
