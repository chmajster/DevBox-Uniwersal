import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { AppLayout } from './layout/AppLayout'
import { AuditPage } from './pages/AuditPage'
import { DashboardPage } from './pages/DashboardPage'
import { JobsPage } from './pages/JobsPage'
import { LoginPage } from './pages/LoginPage'
import { ProjectDetailPage } from './pages/ProjectDetailPage'
import { ProjectWizardPage } from './pages/ProjectWizardPage'
import { ProjectsPage } from './pages/ProjectsPage'

export function App() {
  return <BrowserRouter><Routes>
    <Route path="/login" element={<LoginPage />} />
    <Route element={<ProtectedRoute />}>
      <Route element={<AppLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path="apps" element={<ProjectsPage />} />
        <Route path="apps/new" element={<ProjectWizardPage />} />
        <Route path="apps/:id" element={<ProjectDetailPage />} />
        <Route path="jobs" element={<JobsPage />} />
        <Route path="audit" element={<AuditPage />} />
      </Route>
    </Route>
  </Routes></BrowserRouter>
}
