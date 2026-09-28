import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { readPreference, savePreference } from '../layout/navigation'

type Theme = 'light' | 'dark'
interface ThemeState { theme: Theme; toggle(): void }
const ThemeContext = createContext<ThemeState | null>(null)
// A new design preference starts in the selected dark style, independent of the old workspace default.
export const THEME_KEY = 'devbox-control-theme'
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => readPreference(THEME_KEY, 'dark') === 'light' ? 'light' : 'dark')
  useEffect(() => {
    document.documentElement.dataset.theme = theme
    document.documentElement.style.colorScheme = theme
    savePreference(THEME_KEY, theme)
  }, [theme])
  const value = useMemo<ThemeState>(() => ({ theme, toggle() { setTheme((current) => current === 'dark' ? 'light' : 'dark') } }), [theme])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
export function useTheme() {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('useTheme must be used inside ThemeProvider')
  return context
}
