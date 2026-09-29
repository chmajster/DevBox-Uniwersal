import { useCallback, useEffect, useMemo, useState } from 'react'
import { apiURL, request } from '../api/client'
import type {
  DatabaseBackup,
  DatabaseBinding,
  DatabaseBindingInput,
  DatabaseMode,
  MySQLPluginStatus,
  PostgreSQLPluginStatus,
  ProjectDatabaseServices,
  Job,
  ProjectRuntimeInfo,
  RuntimeContainerConfig,
} from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { databaseModeFields } from './databaseMode'
import { effectiveRuntimeName, preparePHPModuleConfig, updatePHPModuleSelection } from './phpModuleConfig'

interface Props {
  projectId: string
}

const emptyDraft: DatabaseBindingInput = {
  mode: 'none',
  application_service: '',
  compose_service: '',
  engine: 'mysql',
  host: '',
  port: 3306,
  database: '',
  username: '',
  password: '',
  password_provided: false,
  host_access_only: false,
}

function bindingToDraft(binding: DatabaseBinding): DatabaseBindingInput {
  return {
    mode: binding.mode,
    application_service: binding.application_service ?? '',
    compose_service: binding.compose_service ?? '',
    engine: binding.engine ?? 'mysql',
    host: binding.host ?? '',
    port: binding.port || 3306,
    database: binding.database ?? '',
    username: binding.username ?? '',
    password: '',
    password_provided: false,
    host_access_only: false,
  }
}

function modeLabel(mode: DatabaseMode) {
  switch (mode) {
    case 'managed': return 'Bazy danych DevBox'
    case 'compose': return 'Baza z Docker Compose'
    case 'external': return 'Zewnętrzny MySQL/MariaDB'
    default: return 'Brak bazy'
  }
}

const databaseModeOptions: Array<{ mode: DatabaseMode; title: string; description: string }> = [
  {
    mode: 'none',
    title: 'Brak bazy danych',
    description: 'Aplikacja nie otrzyma konfiguracji połączenia do bazy.',
  },
  {
    mode: 'managed',
    title: 'Bazy danych DevBox',
    description: 'Wybierz MySQL/MariaDB, PostgreSQL albo oba serwery. Dane połączenia są stałe; bazy, konta i uprawnienia zarządzasz w module Bazy danych.',
  },
  {
    mode: 'external',
    title: 'Zewnętrzny MySQL / MariaDB',
    description: 'Połącz aplikację z istniejącym serwerem osiągalnym z kontenera przez DNS lub IP.',
  },
  {
    mode: 'compose',
    title: 'Baza z Docker Compose projektu',
    description: 'Użyj serwisu bazy zdefiniowanego w compose.yaml lub docker-compose.yml, np. db:3306.',
  },
]

type DatabaseServiceSelection = 'mysql' | 'postgresql' | 'both'

function serviceSelectionFromEngines(engines: string[] | undefined, fallbackEngine = 'mysql'): DatabaseServiceSelection {
  const normalized = new Set((engines ?? []).map((engine) => engine.toLowerCase()))
  if (normalized.has('mysql') && normalized.has('postgresql')) return 'both'
  if (normalized.has('postgresql')) return 'postgresql'
  if (normalized.has('mysql')) return 'mysql'
  return fallbackEngine.toLowerCase() === 'postgresql' || fallbackEngine.toLowerCase() === 'postgres' ? 'postgresql' : 'mysql'
}

function serviceEngines(selection: DatabaseServiceSelection): Array<'mysql' | 'postgresql'> {
  if (selection === 'both') return ['mysql', 'postgresql']
  return [selection]
}

export function ProjectDatabaseSection({ projectId }: Props) {
  const { user } = useAuth()
  const readOnly = user?.role === 'viewer'
  const [binding, setBinding] = useState<DatabaseBinding | null>(null)
  const [draft, setDraft] = useState<DatabaseBindingInput>(emptyDraft)
  const [composeServices, setComposeServices] = useState<string[]>([])
  const [managedMySQL, setManagedMySQL] = useState<MySQLPluginStatus | null>(null)
  const [managedPostgreSQL, setManagedPostgreSQL] = useState<PostgreSQLPluginStatus | null>(null)
  const [databaseServices, setDatabaseServices] = useState<ProjectDatabaseServices | null>(null)
  const [serviceSelection, setServiceSelection] = useState<DatabaseServiceSelection>('mysql')
  const [runtimeConfig, setRuntimeConfig] = useState<RuntimeContainerConfig | null>(null)
  const [runtimeInfo, setRuntimeInfo] = useState<ProjectRuntimeInfo | null>(null)
  const [backups, setBackups] = useState<DatabaseBackup[]>([])
  const [showBackups, setShowBackups] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    const [currentBinding, config, runtime, mysqlStatus, postgresqlStatus, selectedServices] = await Promise.all([
      request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`),
      request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`).catch(() => null),
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`).catch(() => null),
      request<MySQLPluginStatus>('/plugins/mysql/status').catch(() => null),
      request<PostgreSQLPluginStatus>('/plugins/postgresql/status').catch(() => null),
      request<ProjectDatabaseServices>(`/projects/${encodeURIComponent(projectId)}/database-services`).catch(() => null),
    ])
    const savedServices = selectedServices?.engines ?? []
    const currentDraft = bindingToDraft(currentBinding)
    if (savedServices.length > 0) currentDraft.mode = 'managed'
    setBinding(currentBinding)
    setDraft(currentDraft)
    setRuntimeConfig(config)
    setRuntimeInfo(runtime)
    setManagedMySQL(mysqlStatus)
    setManagedPostgreSQL(postgresqlStatus)
    setDatabaseServices(selectedServices)
    setServiceSelection(serviceSelectionFromEngines(savedServices, currentBinding.engine ?? 'mysql'))
  }, [projectId])

  const loadComposeServices = useCallback(async () => {
    try {
      const services = await request<string[]>(`/projects/${encodeURIComponent(projectId)}/database-binding/compose-services`)
      setComposeServices(services ?? [])
    } catch {
      setComposeServices([])
    }
  }, [projectId])

  useEffect(() => {
    setError('')
    setMessage('')
    void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać konfiguracji bazy danych'))
    void loadComposeServices()
  }, [load, loadComposeServices])

  const modeFields = databaseModeFields(draft.mode)
  const effectiveRuntime = effectiveRuntimeName(runtimeConfig?.runtime, runtimeInfo?.runtime)
  const phpModules = useMemo(() => new Set((runtimeConfig?.modules ?? []).map((item) => item.name.toLowerCase())), [runtimeConfig])
  const selectedEngine = (draft.engine || binding?.engine || 'mysql').toLowerCase()
  const selectedServices = serviceEngines(serviceSelection)
  const usesMySQL = draft.mode === 'managed'
    ? selectedServices.includes('mysql')
    : draft.mode !== 'none' && (selectedEngine === 'mysql' || selectedEngine === 'mariadb')
  const usesPostgreSQL = draft.mode === 'managed' && selectedServices.includes('postgresql')
  const needsPHPMySQLDriver = effectiveRuntime === 'php' && usesMySQL && !phpModules.has('pdo_mysql') && !phpModules.has('mysqli')
  const needsPHPPostgreSQLDriver = effectiveRuntime === 'php' && usesPostgreSQL && !phpModules.has('pgsql') && !phpModules.has('pdo_pgsql')
  const managedMySQLMissing = managedMySQL !== null && !managedMySQL.installed
  const managedPostgreSQLMissing = managedPostgreSQL !== null && !managedPostgreSQL.installed


  async function perform(name: string, action: () => Promise<void>) {
    setBusy(name)
    setError('')
    setMessage('')
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operacja bazy danych nie powiodła się')
    } finally {
      setBusy('')
    }
  }

  async function saveBinding() {
    await perform('save', async () => {
      if (draft.mode === 'managed') {
        await request<ProjectDatabaseServices>(`/projects/${encodeURIComponent(projectId)}/database-services`, {
          method: 'PUT',
          body: JSON.stringify({ engines: serviceEngines(serviceSelection) }),
        })
        if (binding?.mode !== 'none') {
          await request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`, { method: 'DELETE' })
        }
        await load()
        setMessage(`Zapisano dostęp do serwerów DevBox: ${serviceSelection === 'both' ? 'MySQL/MariaDB i PostgreSQL' : serviceSelection === 'postgresql' ? 'PostgreSQL' : 'MySQL/MariaDB'}.`)
        return
      }

      await request<ProjectDatabaseServices>(`/projects/${encodeURIComponent(projectId)}/database-services`, {
        method: 'PUT',
        body: JSON.stringify({ engines: [] }),
      })
      if (draft.mode === 'none') {
        await request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`, { method: 'DELETE' })
      } else {
        await request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`, {
          method: 'PUT',
          body: JSON.stringify(draft),
        })
      }
      await load()
      setMessage(`Tryb bazy został zapisany: ${modeLabel(draft.mode)}.`)
    })
  }

  async function testConnection() {
    await perform('test', async () => {
      await request<{ status: string }>(`/projects/${encodeURIComponent(projectId)}/database-binding/test`, {
        method: 'POST',
        body: '{}',
      })
      setMessage('Połączenie z bazą działa — SELECT 1 zakończył się powodzeniem.')
      await load()
    })
  }

  async function addPHPDatabaseDriver(driver: 'pdo_mysql' | 'pgsql') {
    if (!runtimeConfig) {
      setError('Nie udało się pobrać konfiguracji runtime projektu. Odśwież widok i spróbuj ponownie.')
      return
    }
    await perform('database-driver', async () => {
      const configWithDriver = {
        ...runtimeConfig,
        modules: updatePHPModuleSelection(runtimeConfig.modules, driver, true),
      }
      const payload = preparePHPModuleConfig(configWithDriver)
      const saved = await request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`, {
        method: 'PUT',
        body: JSON.stringify(payload),
      })
      setRuntimeConfig(saved)
      setMessage(driver === 'pgsql'
        ? 'Sterownik PostgreSQL dodano do runtime PHP. Zostanie zainstalowany przy przebudowie obrazu.'
        : 'PDO MySQL dodano do runtime PHP. Zostanie zainstalowane przy przebudowie obrazu.')
    })
  }

  async function loadBackups() {
    if (!binding?.database_id) return
    const items = await request<DatabaseBackup[]>(`/databases/${encodeURIComponent(binding.database_id)}/backups`)
    setBackups(items ?? [])
    setShowBackups(true)
  }

  async function queueBackup() {
    if (!binding?.database_id) return
    await perform('backup', async () => {
      const result = await request<{ job: Job; backup: DatabaseBackup }>(`/databases/${encodeURIComponent(binding.database_id!)}/backup`, { method: 'POST', body: '{}' })
      setMessage(`Backup dodano do kolejki jako zadanie ${result.job.id}.`)
      await loadBackups()
    })
  }

  async function restoreBackup(backup: DatabaseBackup) {
    if (!binding?.database_id) return
    if (!window.confirm(`Przywrócić backup ${backup.file_name} do przypisanej bazy projektu?`)) return
    await perform('restore', async () => {
      const job = await request<Job>(`/databases/${encodeURIComponent(binding.database_id!)}/restore`, {
        method: 'POST',
        body: JSON.stringify({ backup_id: backup.id }),
      })
      setMessage(`Restore dodano do kolejki jako zadanie ${job.id}.`)
    })
  }

  async function deleteBackup(backup: DatabaseBackup) {
    await perform('delete-backup', async () => {
      await request<{ status: string }>(`/database-backups/${encodeURIComponent(backup.id)}`, { method: 'DELETE' })
      await loadBackups()
    })
  }

  const serviceOptions = composeServices.length > 0
    ? composeServices
    : [draft.application_service, draft.compose_service].filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index)

  const managedExists = binding?.mode === 'managed' && Boolean(binding.database_id)

  return <section className="panel runtime-section">
    <div className="section-heading">
      <div>
        <h2>Baza danych</h2>
        <p className="muted">Bazy instalowane z Pluginów działają jako trwałe kontenery Docker. Aplikacje łączą się z MySQL/MariaDB przez <code>devbox-mysql:3306</code> w sieci <code>devbox-apps</code>; nie używają hostowego MySQL ani adresów IP hosta.</p>
      </div>
      <span className="status-chip" data-ok={binding?.status === 'ready' || binding?.status === 'configured' ? 'true' : 'false'}>
        {binding?.mode ? modeLabel(binding.mode) : 'Ładowanie'}
      </span>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    <fieldset disabled={readOnly || busy !== ''} className="database-mode-selector">
      <legend>Tryb bazy danych</legend>
      <div className="database-mode-options">
        {databaseModeOptions.map(({ mode, title, description }) =>
          <label className="database-mode-option" data-selected={draft.mode === mode ? 'true' : 'false'} key={mode}>
            <input
              type="radio"
              name={`database-mode-${projectId}`}
              checked={draft.mode === mode}
              onChange={() => setDraft({
                ...emptyDraft,
                mode,
                engine: 'mysql',
                port: 3306,
                host_access_only: false,
                application_service: draft.application_service,
              })}
            />
            <span className="database-mode-copy"><strong>{title}</strong><small>{description}</small></span>
          </label>
        )}
      </div>
    </fieldset>

    {draft.mode !== 'none' && <div className="form-grid">
      {modeFields.includes('application_service') && <label>Application service
        <select disabled={readOnly || busy !== ''} value={draft.application_service ?? ''} onChange={(event) => setDraft({ ...draft, application_service: event.target.value })}>
          <option value="">Automatycznie wykryj</option>
          {serviceOptions.map((service) => <option key={service} value={service}>{service}</option>)}
        </select>
      </label>}

      {draft.mode === 'managed' && <>
        <div className="validation-box span-2">
          <strong>Wspólny MySQL/MariaDB DevBox działa w osobnym kontenerze Docker.</strong>
          <span>Aplikacja otrzyma adres <code>{managedMySQL?.container_host ?? binding?.application_host ?? 'devbox-mysql'}:{managedMySQL?.port ?? binding?.application_port ?? 3306}</code> i połączy się przez sieć <code>{managedMySQL?.network ?? 'devbox-apps'}</code>. DevBox utworzy tylko bazę, użytkownika i hasło; serwer SQL jest współdzielony przez wiele aplikacji.</span>
        </div>
        <label>Serwer SQL<input readOnly value={managedMySQL === null ? 'status niedostępny' : !managedMySQL.installed ? 'niezainstalowany — użyj Pluginów' : managedMySQL.running ? 'zainstalowany i uruchomiony' : 'zainstalowany — zostanie uruchomiony przy użyciu'} /></label>
        <label>Adres dla aplikacji<input readOnly value={`${managedMySQL?.container_host ?? binding?.application_host ?? 'devbox-mysql'}:${managedMySQL?.port ?? binding?.application_port ?? 3306}`} /></label>
        <label>Sieć Docker<input readOnly value={managedMySQL?.network ?? 'devbox-apps'} /></label>
        <label>Nazwa bazy<input readOnly value={binding?.database || 'zostanie utworzona automatycznie'} /></label>
        <label>Użytkownik<input readOnly value={binding?.username || 'zostanie utworzony automatycznie'} /></label>
        <label>Status bazy<input readOnly value={binding?.status || 'jeszcze nie utworzono'} /></label>
        {managedServerKnownMissing && <div className="validation-box span-2">
          <strong>Serwer MySQL/MariaDB DevBox nie jest zainstalowany.</strong>
          <span>Zainstaluj go w zakładce Pluginy. DevBox nie instaluje już MySQL/MariaDB jako usługi systemowej hosta.</span>
        </div>}
      </>}

      {modeFields.includes('compose_service') && <>
        <label>Database service
          <select disabled={readOnly || busy !== ''} required value={draft.compose_service ?? ''} onChange={(event) => setDraft({ ...draft, compose_service: event.target.value })}>
            <option value="">Wybierz service bazy</option>
            {serviceOptions.map((service) => <option key={service} value={service}>{service}</option>)}
          </select>
        </label>
        <label>Port wewnętrzny<input disabled={readOnly || busy !== ''} type="number" min={1} max={65535} value={draft.port ?? 3306} onChange={(event) => setDraft({ ...draft, port: Number(event.target.value) })} /></label>
        <label>Nazwa bazy<input disabled={readOnly || busy !== ''} value={draft.database ?? ''} onChange={(event) => setDraft({ ...draft, database: event.target.value })} /></label>
        <label>Użytkownik<input disabled={readOnly || busy !== ''} value={draft.username ?? ''} onChange={(event) => setDraft({ ...draft, username: event.target.value })} /></label>
        <label className="span-2">Hasło / Secret
          <input disabled={readOnly || busy !== '' || draft.password_provided} type="password" autoComplete="new-password" value={draft.password ?? ''} onChange={(event) => setDraft({ ...draft, password: event.target.value, password_provided: false })} placeholder={draft.password_provided ? 'połączenie bez hasła' : binding?.has_secret ? 'pozostaw puste, aby zachować obecny SecretStore secret' : 'hasło użytkownika bazy'} />
        </label>
        <label className="checkbox span-2"><input disabled={readOnly || busy !== ''} type="checkbox" checked={Boolean(draft.password_provided)} onChange={(event) => setDraft({ ...draft, password: '', password_provided: event.target.checked })} /> Użytkownik bazy nie ma hasła</label>
      </>}

      {modeFields.includes('host') && draft.mode === 'external' && <>
        <div className="validation-box span-2">
          <strong>Zewnętrzny serwer MySQL/MariaDB.</strong>
          <span>Użyj tego trybu tylko dla serwera spoza zarządzanego MySQL DevBox. Podaj DNS lub IP osiągalne bezpośrednio z kontenera aplikacji.</span>
        </div>
        <label>Host MySQL/MariaDB<input disabled={readOnly || busy !== ''} value={draft.host ?? ''} onChange={(event) => setDraft({ ...draft, host: event.target.value, host_access_only: false })} placeholder="np. mysql.example.internal" /></label>
        <label>Port<input disabled={readOnly || busy !== ''} type="number" min={1} max={65535} value={draft.port ?? 3306} onChange={(event) => setDraft({ ...draft, port: Number(event.target.value), host_access_only: false })} /></label>
        <label>Nazwa istniejącej bazy<input disabled={readOnly || busy !== ''} value={draft.database ?? ''} onChange={(event) => setDraft({ ...draft, database: event.target.value, host_access_only: false })} placeholder="np. wordpress" /></label>
        <label>Użytkownik bazy<input disabled={readOnly || busy !== ''} value={draft.username ?? ''} onChange={(event) => setDraft({ ...draft, username: event.target.value, host_access_only: false })} /></label>
        <label className="span-2">Hasło / Secret
          <input disabled={readOnly || busy !== '' || draft.password_provided} type="password" autoComplete="new-password" value={draft.password ?? ''} onChange={(event) => setDraft({ ...draft, password: event.target.value, password_provided: false, host_access_only: false })} placeholder={draft.password_provided ? 'połączenie bez hasła' : binding?.has_secret ? 'pozostaw puste, aby zachować obecny SecretStore secret' : 'hasło do istniejącej bazy'} />
        </label>
        <label className="checkbox span-2"><input disabled={readOnly || busy !== ''} type="checkbox" checked={Boolean(draft.password_provided)} onChange={(event) => setDraft({ ...draft, password: '', password_provided: event.target.checked, host_access_only: false })} /> Użytkownik bazy nie ma hasła</label>
      </>}

      {modeFields.includes('application_host') && <label>Host używany przez aplikację
        <input readOnly value={draft.mode === 'managed'
          ? (binding?.application_host || 'devbox-mysql')
          : draft.mode === 'compose'
            ? (draft.compose_service || '—')
            : draft.mode === 'external'
              ? (['127.0.0.1', 'localhost', '::1'].includes((draft.host || '').trim().toLowerCase()) ? 'host.docker.internal' : (binding?.mode === 'external' && binding?.application_host && binding.host === draft.host ? binding.application_host : (draft.host || '—')))
              : '—'} />
      </label>}
      {modeFields.includes('application_port') && <label>Port używany przez aplikację<input readOnly value={draft.mode === 'managed' ? (binding?.application_port || 3306) : (draft.port || 3306)} /></label>}
    </div>}

    {needsPHPMySQLDriver && <div className="validation-box">
      <strong>Projekt korzysta z MySQL/MariaDB, ale kontener PHP nie posiada wybranego sterownika.</strong>
      {!readOnly && <div className="actions"><button type="button" className="secondary" disabled={busy !== '' || !runtimeConfig} onClick={() => void addPHPDatabaseDriver('pdo_mysql')}>Dodaj PDO MySQL</button></div>}
    </div>}


    {!readOnly && <div className="actions">
      {draft.mode === 'managed' && !managedExists
        ? <button type="button" disabled={busy !== '' || managedServerKnownMissing} onClick={() => void provisionManaged()}>{busy === 'provision' ? 'Tworzenie bazy…' : 'Utwórz bazę i połącz z aplikacją'}</button>
        : <button type="button" disabled={busy !== ''} onClick={() => void saveBinding()}>{busy === 'save' ? 'Zapisywanie…' : 'Zapisz konfigurację bazy'}</button>}
      {binding?.mode !== 'none' && !binding?.host_access_only && <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void testConnection()}>{busy === 'test' ? 'Testowanie…' : 'Testuj połączenie'}</button>}
      {binding?.mode === 'managed' && <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void openPHPMyAdmin()}>Otwórz phpMyAdmin</button>}
      {binding?.mode === 'managed' && <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void rotatePassword()}>Zmień hasło</button>}
      {binding?.database_id && <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void perform('backups', loadBackups)}>Kopie zapasowe</button>}
    </div>}

    {showBackups && binding?.database_id && <div className="backup-panel">
      <div className="section-heading">
        <div><h3>Kopie zapasowe przypisanej bazy</h3><p className="muted">{binding.database || binding.database_id}</p></div>
        {!readOnly && <button type="button" disabled={busy !== ''} onClick={() => void queueBackup()}>Utwórz kopię</button>}
      </div>
      <div className="table-scroll">
        <table>
          <thead><tr><th>Plik</th><th>Status</th><th>Utworzono</th><th>Akcje</th></tr></thead>
          <tbody>
            {backups.map((backup) => <tr key={backup.id}>
              <td>{backup.file_name}</td>
              <td>{backup.status}</td>
              <td>{new Date(backup.created_at).toLocaleString('pl-PL')}</td>
              <td className="actions">
                {backup.status === 'ready' && <button type="button" className="secondary" onClick={() => window.open(apiURL(`/database-backups/${encodeURIComponent(backup.id)}/download`), '_blank', 'noopener,noreferrer')}>Pobierz</button>}
                {!readOnly && backup.status === 'ready' && <button type="button" className="secondary" disabled={busy !== ''} onClick={() => void restoreBackup(backup)}>Restore</button>}
                {!readOnly && <button type="button" className="danger" disabled={busy !== ''} onClick={() => void deleteBackup(backup)}>Usuń</button>}
              </td>
            </tr>)}
            {backups.length === 0 && <tr><td colSpan={4} className="muted">Brak kopii zapasowych dla tej bazy.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>}
  </section>
}
