import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { AppLayout } from './layout/AppLayout'
import { AuditPage } from './pages/AuditPage'
import { BackupsPage } from './pages/BackupsPage'
import { DashboardPage } from './pages/DashboardPage'
import { DatabasesPage } from './pages/DatabasesPage'
import { DatabaseEnginePage } from './pages/DatabaseEnginePage'
import { DatabaseUserAccountPage } from './pages/DatabaseUserAccountPage'
import { DatabaseUsersPage } from './pages/DatabaseUsersPage'
import { CredentialsPage } from './pages/CredentialsPage'
import { DockerPage } from './pages/DockerPage'
import { DomainsPage } from './pages/DomainsPage'
import { JobsPage } from './pages/JobsPage'
import { HealthPage } from './pages/HealthPage'
import { LogsPage } from './pages/LogsPage'
import { LoginPage } from './pages/LoginPage'
import { PortsPage } from './pages/PortsPage'
import { PluginsPage } from './pages/PluginsPage'
import { ApplicationDetailPage } from './pages/ApplicationDetailPage'
import { ApplicationWizardPage } from './pages/ApplicationWizardPage'
import { ApplicationsPage } from './pages/ApplicationsPage'
import { ROUTES } from './routes'
import { RuntimeManagerPage } from './pages/RuntimeManagerPage'
import { UpdatesPage } from './pages/UpdatesPage'
import { UsersPage } from './pages/UsersPage'

export function App() {
  return <BrowserRouter><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path={ROUTES.applications} element={<ApplicationsPage />} />
        <Route path={ROUTES.credentials} element={<CredentialsPage />} />
        <Route path={ROUTES.users} element={<UsersPage />} />
        <Route path="/applications" element={<Navigate to={ROUTES.applications} replace />} />
        <Route path="apps/new" element={<ApplicationWizardPage />} />
        <Route path="apps/:id" element={<ApplicationDetailPage />} />
        <Route path={ROUTES.logs} element={<LogsPage />} />
        <Route path={ROUTES.health} element={<HealthPage />} />
        <Route path={ROUTES.backups} element={<BackupsPage />} />
        <Route path="databases" element={<DatabasesPage />} />
        <Route path="databases/:engine" element={<DatabaseEnginePage />} />
        <Route path="databases/:engine/users/:userId" element={<DatabaseUserAccountPage />} />
        <Route path={ROUTES.databaseUsers} element={<DatabaseUsersPage />} />
        <Route path="domains" element={<DomainsPage />} />
        <Route path="ports" element={<PortsPage />} />
        <Route path={ROUTES.plugins} element={<PluginsPage />} />
        <Route path="docker" element={<DockerPage />} />
        <Route path="runtimes" element={<RuntimeManagerPage />} />
        <Route path="projects/:projectId/runtime" element={<Navigate to="/apps" replace />} />
        <Route path={ROUTES.jobs} element={<JobsPage />} />
        <Route path={ROUTES.audit} element={<AuditPage />} />
        <Route path={ROUTES.updates} element={<UpdatesPage />} />
      </Route>
    </Route>
  </Routes></BrowserRouter>
}
