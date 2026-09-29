import type { UpdateProgress } from '../api/types'

export const UPDATE_STAGES = [
  { id: 'starting', label: 'Start', percent: 2, description: 'Uruchomienie usługi aktualizacji i inicjalizacja stanu.' },
  { id: 'source_check', label: 'Sprawdzenie źródła', percent: 8, description: 'Weryfikacja konfiguracji i źródła aktualizacji.' },
  { id: 'download', label: 'Pobieranie', percent: 15, description: 'Pobranie podpisanego wydania albo jawnie wybranych źródeł Git.' },
  { id: 'validation', label: 'Walidacja', percent: 30, description: 'Podpis Ed25519, SHA-256 i bezpieczne archiwum w trybie signed; w trybie Git kontrola wymaganych plików.' },
  { id: 'snapshot', label: 'Kopia przed migracją', percent: 32, description: 'Odmowa aktualizacji przy aktywnych zadaniach; szyfrowana kopia plików, konfiguracji i SQLite.' },
  { id: 'environment', label: 'System i WSL', percent: 38, description: 'Detekcja systemu, WSL i systemd.' },
  { id: 'dependencies', label: 'Komponenty', percent: 46, description: 'Weryfikacja pakietów, Dockera/Compose i źródeł.' },
  { id: 'backend', label: 'Build backendu', percent: 58, description: 'Budowanie binarek Go: devbox i devbox-helper.' },
  { id: 'frontend', label: 'Build frontendu', percent: 68, description: 'npm ci oraz produkcyjny build SPA.' },
  { id: 'artifacts', label: 'Instalacja plików', percent: 78, description: 'Instalacja binarek, frontendu, migracji i konfiguracji.' },
  { id: 'service', label: 'Usługi systemd', percent: 87, description: 'Aktualizacja unitów i uruchomienie bieżącej usługi.' },
  { id: 'healthcheck', label: 'Healthcheck', percent: 94, description: 'Kontrola API oraz diagnostyka devbox doctor.' },
  { id: 'summary', label: 'Finalizacja', percent: 98, description: 'Końcowe kroki instalatora i podsumowanie.' },
  { id: 'restart', label: 'Restart', percent: 99, description: 'Końcowy restart i potwierdzenie wersji oraz gotowości SQLite przez API.' },
  { id: 'completed', label: 'Gotowe', percent: 100, description: 'Aktualizacja zakończona.' },
] as const

export type UpdateStageState = 'pending' | 'current' | 'done' | 'failed' | 'skipped'

const UPDATE_REFRESH_PARAM = '__devbox_refresh'

export function buildUpdateHardRefreshURL(href: string, token: string | number): string {
  const url = new URL(href)
  url.searchParams.set(UPDATE_REFRESH_PARAM, String(token))
  return url.toString()
}

export function clearUpdateHardRefreshURL(href: string): string {
  const url = new URL(href)
  url.searchParams.delete(UPDATE_REFRESH_PARAM)
  return url.toString()
}

export function updateIsActive(progress?: UpdateProgress | null): boolean {
  return progress?.state === 'starting' || progress?.state === 'running'
}

export function clampUpdatePercent(value?: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, Math.round(value ?? 0)))
}

export function updateStateLabel(progress?: UpdateProgress | null): string {
  switch (progress?.state) {
    case 'starting':
    case 'running':
      return 'Aktualizacja w toku'
    case 'succeeded':
      return 'Zakończona'
    case 'failed':
      return 'Błąd aktualizacji'
    case 'no_update':
      return 'Brak zmian'
    case 'unknown':
      return 'Stan nieznany'
    default:
      return 'Bezczynny'
  }
}

export function updateStageState(progress: UpdateProgress | null | undefined, stageId: string): UpdateStageState {
  const stageIndex = UPDATE_STAGES.findIndex((stage) => stage.id === stageId)
  if (stageIndex < 0 || !progress || progress.state === 'idle' || progress.state === 'unknown') return 'pending'

  if (progress.stage === 'rollback') return stageId === 'completed' ? 'pending' : 'skipped'
  if (progress.state === 'succeeded') return 'done'
  if (progress.state === 'no_update') {
    const downloadIndex = UPDATE_STAGES.findIndex((stage) => stage.id === 'download')
    const completedIndex = UPDATE_STAGES.findIndex((stage) => stage.id === 'completed')
    if (stageIndex <= downloadIndex || stageIndex === completedIndex) return 'done'
    return 'skipped'
  }

  const currentIndex = UPDATE_STAGES.findIndex((stage) => stage.id === progress.stage)
  if (currentIndex >= 0) {
    if (stageIndex < currentIndex) return 'done'
    if (stageIndex === currentIndex) return progress.state === 'failed' ? 'failed' : 'current'
    return 'pending'
  }

  const stage = UPDATE_STAGES[stageIndex]
  if (clampUpdatePercent(progress.percent) > stage.percent) return 'done'
  return 'pending'
}
