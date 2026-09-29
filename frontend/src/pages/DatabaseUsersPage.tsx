import { Navigate } from 'react-router-dom'

export function DatabaseUsersPage() {
  return <Navigate to="/databases/mysql?tab=users" replace />
}
