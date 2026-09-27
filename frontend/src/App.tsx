import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { AppLayout } from './layout/AppLayout'
import { ApplicationsPage } from './pages/ApplicationsPage'
import { AuditPage } from './pages/AuditPage'
import { DashboardPage } from './pages/DashboardPage'
import { JobsPage } from './pages/JobsPage'
import { LogsPage } from './pages/LogsPage'
import { LoginPage } from './pages/LoginPage'
import { ProjectDetailsPage } from './pages/ProjectDetailsPage'
import { ROUTES } from './routes'

export function App() {
  return <BrowserRouter><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route path={ROUTES.dashboard} element={<DashboardPage />} />
        <Route path={ROUTES.applications} element={<ApplicationsPage />} />
        <Route path={ROUTES.project} element={<ProjectDetailsPage />} />
        <Route path={ROUTES.logs} element={<LogsPage />} />
        <Route path={ROUTES.jobs} element={<JobsPage />} />
        <Route path={ROUTES.audit} element={<AuditPage />} />
      </Route>
    </Route>
  </Routes></BrowserRouter>
}
