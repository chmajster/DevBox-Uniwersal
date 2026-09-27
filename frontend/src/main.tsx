import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { AuthProvider } from './auth/AuthContext'
import { ThemeProvider } from './theme/ThemeProvider'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <AuthProvider><App /></AuthProvider>
    </ThemeProvider>
  </StrictMode>
)
