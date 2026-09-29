import { useCallback, useEffect, useMemo, useState } from 'react'
import { apiURL, request } from '../api/client'
import type {
  DatabaseBackup,
  DatabaseBinding,
  DatabaseBindingInput,
  DatabaseMode,
  HostDatabaseInstance,
  Job,
  PHPMyAdminStatus,
  ProjectRuntimeInfo,
  RuntimeContainerConfig,
} from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { databaseModeChoice, databaseModeFields, dockerHostDatabaseHost, type DatabaseModeChoice } from './databaseMode'
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
    host_access_only: binding.host_access_only ?? false,
  }
}

function modeLabel(mode: DatabaseMode, hostAccessOnly = false) {
  if (databaseModeChoice(mode, hostAccessOnly) === 'host') return 'Baza na hoście — dostęp sieciowy'
  switch (mode) {
    case 'managed': return 'Nowa baza w DevBox'
    case 'compose': return 'Baza z Docker Compose'
    case 'external': return 'Istniejący host MySQL/MariaDB'
    default: return 'Brak bazy'
  }
}

function formatDatabaseTimestamp(value?: string) {
  if (!value || value.startsWith('0001-01-01')) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime()) || parsed.getUTCFullYear() <= 1) return '—'
  return parsed.toLocaleString('pl-PL')
}

const databaseModeOptions: Array<{ choice: DatabaseModeChoice; mode: DatabaseMode; title: string; description: string }> = [
  {
    choice: 'none',
    mode: 'none',
    title: 'Brak bazy danych',
    description: 'Aplikacja nie otrzyma konfiguracji połączenia do bazy.',
  },
  {
    choice: 'managed',
    mode: 'managed',
    title: 'Utwórz nową bazę w DevBox',
    description: 'DevBox utworzy bazę, użytkownika i hasło na wspólnym MySQL devbox-mysql:3306.',
  },
  {
    choice: 'host',
    mode: 'external',
    title: 'Połącz z bazą na hoście',
    description: 'Wybierz wykryty MySQL/MariaDB albo PostgreSQL i port. DevBox otworzy kontenerowi drogę przez host.docker.internal.',
  },
  {
    choice: 'external',
    mode: 'external',
    title: 'Użyj istniejącej bazy MySQL/MariaDB',
    description: 'Podaj adres zewnętrznego serwera, port, nazwę istniejącej bazy, użytkownika i hasło.',
  },
  {
    choice: 'compose',
    mode: 'compose',
    title: 'Użyj bazy z Docker Compose projektu',
    description: 'Połącz aplikację z service bazy zdefiniowanym w Compose, np. db:3306.',
  },
]

export function ProjectDatabaseSection({ projectId }: Props) {
  const { user } = useAuth()
  const readOnly = user?.role === 'viewer'
  const [binding, setBinding] = useState<DatabaseBinding | null>(null)
  const [draft, setDraft] = useState<DatabaseBindingInput>(emptyDraft)
  const [composeServices, setComposeServices] = useState<string[]>([])
  const [hostDatabases, setHostDatabases] = useState<HostDatabaseInstance[]>([])
  const [hostDatabasesLoading, setHostDatabasesLoading] = useState(false)
  const [runtimeConfig, setRuntimeConfig] = useState<RuntimeContainerConfig | null>(null)
  const [runtimeInfo, setRuntimeInfo] = useState<ProjectRuntimeInfo | null>(null)
  const [backups, setBackups] = useState<DatabaseBackup[]>([])
  const [showBackups, setShowBackups] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    const [currentBinding, config, runtime] = await Promise.all([
      request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`),
      request<RuntimeContainerConfig>(`/projects/${encodeURIComponent(projectId)}/runtime/config`).catch(() => null),
      request<ProjectRuntimeInfo>(`/projects/${encodeURIComponent(projectId)}/runtime`).catch(() => null),
    ])
    setBinding(currentBinding)
    setDraft(bindingToDraft(currentBinding))
    setRuntimeConfig(config)
    setRuntimeInfo(runtime)
  }, [projectId])

  const loadHostDatabases = useCallback(async () => {
    setHostDatabasesLoading(true)
    try {
      const items = await request<HostDatabaseInstance[]>('/plugins/databases/host')
      setHostDatabases((items ?? []).filter((item) => item.installed && item.application_ready && item.purpose === 'applications'))
    } catch {
      setHostDatabases([])
    } finally {
      setHostDatabasesLoading(false)
    }
  }, [])

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
    void loadHostDatabases()
  }, [load, loadComposeServices, loadHostDatabases])

  const modeFields = databaseModeFields(draft.mode)
  const selectedModeChoice = databaseModeChoice(draft.mode, Boolean(draft.host_access_only))
  const hostDatabaseSelected = selectedModeChoice === 'host'
  const effectiveRuntime = effectiveRuntimeName(runtimeConfig?.runtime, runtimeInfo?.runtime)
  const phpModules = useMemo(() => new Set((runtimeConfig?.modules ?? []).map((item) => item.name.toLowerCase())), [runtimeConfig])
  const selectedEngine = (draft.engine || 'mysql').toLowerCase()
  const needsPHPMySQLDriver = draft.mode !== 'none' && effectiveRuntime === 'php' &&
    (selectedEngine === 'mysql' || selectedEngine === 'mariadb') &&
    !phpModules.has('pdo_mysql') && !phpModules.has('mysqli')
  const needsPHPPostgreSQLDriver = hostDatabaseSelected && effectiveRuntime === 'php' &&
    selectedEngine === 'postgresql' && !phpModules.has('pgsql')
  const selectedHostDatabase = hostDatabases.find((item) => item.engine === selectedEngine && item.port === (draft.port || 0))

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
      if (draft.mode === 'none') {
        await request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`, { method: 'DELETE' })
      } else {
        await request<DatabaseBinding>(`/projects/${encodeURIComponent(projectId)}/database-binding`, {
          method: 'PUT',
          body: JSON.stringify(draft),
        })
      }
      await load()
      setMessage(`Tryb bazy został zapisany: ${modeLabel(draft.mode, Boolean(draft.host_access_only))}.`)
    })
  }

  async function provisionManaged() {
    await perform('provision', async () => {
      await request<unknown>(`/projects/${encodeURIComponent(projectId)}/database/provision`, {
        method: 'POST',
        body: JSON.stringify({
          engine: draft.engine || 'mysql',
          charset: 'utf8mb4',
          application_service: draft.application_service || '',
        }),
      })
      await load()
      setMessage('Baza, użytkownik i ograniczone granty zostały utworzone. Poświadczenia zapisano w SecretStore.')
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

  async function rotatePassword() {
    if (!window.confirm('Zmienić hasło zarządzanego użytkownika bazy? Kolejny deploy otrzyma nowe hasło z SecretStore.')) return
    await perform('password', async () => {
      await request<{ status: string }>(`/projects/${encodeURIComponent(projectId)}/database-binding/password`, {
        method: 'POST',
        body: '{}',
      })
      setMessage('Hasło użytkownika bazy zostało zmienione i zapisane w SecretStore.')
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

  async function openPHPMyAdmin() {
    await perform('phpmyadmin', async () => {
      let status = await request<PHPMyAdminStatus>('/phpmyadmin/install', { method: 'POST', body: '{}' })
      if (!status.running) {
        status = await request<PHPMyAdminStatus>('/phpmyadmin/start', { method: 'POST', body: '{}' })
      }
      if (!status.url) throw new Error('phpMyAdmin nie zwrócił adresu aplikacji')
      window.open(status.url, '_blank', 'noopener,noreferrer')
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
        <p className="muted">Wybierz, czy DevBox ma utworzyć nową bazę dla aplikacji, czy aplikacja ma korzystać z już istniejącego hosta MySQL/MariaDB.</p>
      </div>
      <span className="status-chip" data-ok={binding?.status === 'ready' || binding?.status === 'configured' ? 'true' : 'false'}>
        {binding?.mode ? modeLabel(binding.mode, Boolean(binding.host_access_only)) : 'Ładowanie'}
      </span>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    <fieldset disabled={readOnly || busy !== ''} className="database-mode-selector">
      <legend>Tryb bazy danych</legend>
      <div className="database-mode-options">
        {databaseModeOptions.map(({ choice, mode, title, description }) =>
          <label className="database-mode-option" data-selected={selectedModeChoice === choice ? 'true' : 'false'} key={choice}>
            <input
              type="radio"
              name={`database-mode-${projectId}`}
              checked={selectedModeChoice === choice}
              onChange={() => {
                const firstHost = choice === 'host'
                  ? (hostDatabases.find((item) => item.application_ready) ?? hostDatabases[0])
                  : undefined
                setDraft({
                  ...emptyDraft,
                  mode,
                  engine: firstHost?.engine ?? 'mysql',
                  host: choice === 'host' ? dockerHostDatabaseHost : '',
                  port: firstHost?.port ?? 3306,
                  host_access_only: choice === 'host',
                  application_service: draft.application_service,
                })
              }}
            />
            <span className="database-mode-copy"><strong>{title}</strong><small>{description}</small></span>
          </label>
        )}
      </div>
    </fieldset>

    {draft.mode !== 'none' && <div className="form-grid">
      {modeFields.includes('application_service') && !hostDatabaseSelected && <label>Application service
        <select disabled={readOnly || busy !== ''} value={draft.application_service ?? ''} onChange={(event) => setDraft({ ...draft, application_service: event.target.value })}>
          <option value="">Automatycznie wykryj</option>
          {serviceOptions.map((service) => <option key={service} value={service}>{service}</option>)}
        </select>
      </label>}

      {modeFields.includes('engine') && draft.mode === 'managed' && <>
        <div className="validation-box span-2">
          <strong>DevBox utworzy bazę automatycznie.</strong>
          <span>Powstanie nowa baza na wspólnym serwerze <code>devbox-mysql:3306</code>, osobny użytkownik oraz hasło zapisane w SecretStore. To jest najprostszy wariant np. dla nowej instalacji WordPress.</span>
        </div>
        <label>Silnik<input readOnly value={binding?.engine || draft.engine || 'mysql'} /></label>
        <label>Nazwa bazy<input readOnly value={binding?.database || 'zostanie utworzona automatycznie'} /></label>
        <label>Użytkownik<input readOnly value={binding?.username || 'zostanie utworzony automatycznie'} /></label>
        <label>Status<input readOnly value={binding?.status || 'jeszcze nie utworzono'} /></label>
        <label>Data utworzenia<input readOnly value={formatDatabaseTimestamp(binding?.created_at)} /></label>
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
        {hostDatabaseSelected ? <>
          <div className="validation-box span-2">
            <strong>Wybierz bazę działającą na hoście.</strong>
            <span>Lista zawiera wyłącznie bazy oznaczone w Pluginach jako gotowe dla aplikacji. Wybór ustawia silnik i port, a kontener dostaje dostęp przez <code>host.docker.internal:host-gateway</code>. Nazwę bazy i credentiale nadal ustawia sama aplikacja.</span>
          </div>
          <label className="span-2">Baza / port na hoście
            <select
              disabled={readOnly || busy !== '' || hostDatabasesLoading}
              value={selectedHostDatabase?.id ?? ''}
              onChange={(event) => {
                const selected = hostDatabases.find((item) => item.id === event.target.value)
                if (!selected) return
                setDraft({ ...draft, engine: selected.engine, host: dockerHostDatabaseHost, port: selected.port, host_access_only: true })
              }}
            >
              <option value="">{hostDatabasesLoading ? 'Wykrywanie baz na hoście…' : 'Wybierz wykrytą bazę'}</option>
              {hostDatabases.map((item) => <option key={item.id} value={item.id}>
                {item.label} — port {item.port}{item.running ? ' — działa' : ' — zatrzymana / niedostępna'}
              </option>)}
            </select>
          </label>
          {hostDatabases.length === 0 && !hostDatabasesLoading && <div className="validation-box span-2">
            <strong>Brak gotowej bazy aplikacyjnej na hoście.</strong>
            <span>Wejdź w <strong>Pluginy → Bazy danych dla aplikacji</strong>, wybierz MySQL/MariaDB i/lub PostgreSQL i skonfiguruj port. Po zakończeniu baza pojawi się tutaj automatycznie.</span>
          </div>}
          <label>Silnik
            <select disabled={readOnly || busy !== ''} value={selectedEngine} onChange={(event) => {
              const engine = event.target.value
              setDraft({ ...draft, engine, port: engine === 'postgresql' ? 5432 : 3306, host: dockerHostDatabaseHost, host_access_only: true })
            }}>
              <option value="mysql">MySQL</option>
              <option value="mariadb">MariaDB</option>
              <option value="postgresql">PostgreSQL</option>
            </select>
          </label>
          <label>Port na hoście
            <input disabled={readOnly || busy !== ''} type="number" min={1} max={65535}
              value={draft.port ?? (selectedEngine === 'postgresql' ? 5432 : 3306)}
              onChange={(event) => setDraft({ ...draft, port: Number(event.target.value), host: dockerHostDatabaseHost, host_access_only: true })} />
          </label>
          <label>Adres używany w kontenerze<input readOnly value={`${dockerHostDatabaseHost}:${draft.port || (selectedEngine === 'postgresql' ? 5432 : 3306)}`} /></label>
          <label>Status wykrytej usługi<input readOnly value={selectedHostDatabase ? (selectedHostDatabase.running ? 'działa' : 'zainstalowana, ale nie odpowiada') : 'port ustawiony ręcznie'} /></label>
          {selectedHostDatabase?.version && <label className="span-2">Wersja<input readOnly value={selectedHostDatabase.version} /></label>}
          <div className="database-host-help span-2">
            <div>
              <strong>Jak ustawić połączenie wewnątrz aplikacji</strong>
              <p>Jako host bazy wpisz <code>host.docker.internal</code>, a nie <code>localhost</code> ani <code>127.0.0.1</code>. W kontenerze adresy loopback wskazują na sam kontener, nie na host DevBox.</p>
            </div>
            <div className="database-host-help-grid">
              <div>
                <span className="database-host-help-label">Przykład .env / konfiguracji aplikacji</span>
                <pre><code>{`DB_HOST=host.docker.internal
DB_PORT=${draft.port || (selectedEngine === 'postgresql' ? 5432 : 3306)}
DB_DATABASE=moja_baza
DB_USERNAME=moj_uzytkownik
DB_PASSWORD=moje_haslo`}</code></pre>
              </div>
              <div>
                <span className="database-host-help-label">Sterownik PHP</span>
                <pre><code>{selectedEngine === 'postgresql'
                  ? 'Moduł: pgsql / pdo_pgsql\nHost: host.docker.internal'
                  : 'Moduł: mysqli lub pdo_mysql\nHost: host.docker.internal'}</code></pre>
              </div>
            </div>
            <p className="database-host-warning"><strong>Wymagane po stronie hosta:</strong> wybrany serwer musi nasłuchiwać na interfejsie dostępnym z Dockera. MySQL/MariaDB wymaga odpowiednich grantów, a PostgreSQL odpowiedniego <code>listen_addresses</code> i reguły <code>pg_hba.conf</code>.</p>
          </div>
        </> : <>
          <div className="validation-box span-2">
            <strong>Użyj istniejącej bazy na innym serwerze.</strong>
            <span>DevBox nie utworzy bazy ani użytkownika. Podaj osiągalny z kontenera adres DNS lub IP serwera MySQL/MariaDB.</span>
          </div>
          <label>Host MySQL/MariaDB<input disabled={readOnly || busy !== ''} value={draft.host ?? ''} onChange={(event) => setDraft({ ...draft, host: event.target.value })} placeholder="np. mysql.example.internal" /></label>
          <label>Port<input disabled={readOnly || busy !== ''} type="number" min={1} max={65535} value={draft.port ?? 3306} onChange={(event) => setDraft({ ...draft, port: Number(event.target.value) })} /></label>
          <label>Nazwa istniejącej bazy<input disabled={readOnly || busy !== ''} value={draft.database ?? ''} onChange={(event) => setDraft({ ...draft, database: event.target.value })} placeholder="np. wordpress" /></label>
          <label>Użytkownik bazy<input disabled={readOnly || busy !== ''} value={draft.username ?? ''} onChange={(event) => setDraft({ ...draft, username: event.target.value })} /></label>
          <label className="span-2">Hasło / Secret
            <input disabled={readOnly || busy !== '' || draft.password_provided} type="password" autoComplete="new-password" value={draft.password ?? ''} onChange={(event) => setDraft({ ...draft, password: event.target.value, password_provided: false })} placeholder={draft.password_provided ? 'połączenie bez hasła' : binding?.has_secret ? 'pozostaw puste, aby zachować obecny SecretStore secret' : 'hasło do istniejącej bazy'} />
          </label>
          <label className="checkbox span-2"><input disabled={readOnly || busy !== ''} type="checkbox" checked={Boolean(draft.password_provided)} onChange={(event) => setDraft({ ...draft, password: '', password_provided: event.target.checked })} /> Użytkownik bazy nie ma hasła</label>
          {!readOnly && <div className="actions span-2">
            <button type="button" className="secondary" disabled={busy !== ''} onClick={() => setDraft({ ...draft, host: 'devbox-mysql', port: 3306 })}>Wspólny MySQL DevBox — devbox-mysql:3306</button>
          </div>}
        </>}
      </>}

      {modeFields.includes('application_host') && !hostDatabaseSelected && <label>Host używany przez aplikację
        <input readOnly value={draft.mode === 'managed'
          ? (binding?.application_host || 'devbox-mysql')
          : draft.mode === 'compose'
            ? (draft.compose_service || '—')
            : draft.mode === 'external'
              ? (['127.0.0.1', 'localhost', '::1'].includes((draft.host || '').trim().toLowerCase()) ? 'host.docker.internal' : (binding?.mode === 'external' && binding?.application_host && binding.host === draft.host ? binding.application_host : (draft.host || '—')))
              : '—'} />
      </label>}
      {modeFields.includes('application_port') && !hostDatabaseSelected && <label>Port używany przez aplikację<input readOnly value={draft.mode === 'managed' ? (binding?.application_port || 3306) : (draft.port || 3306)} /></label>}
    </div>}

    {needsPHPMySQLDriver && <div className="validation-box">
      <strong>Projekt korzysta z MySQL/MariaDB, ale kontener PHP nie posiada wybranego sterownika.</strong>
      {!readOnly && <div className="actions"><button type="button" className="secondary" disabled={busy !== '' || !runtimeConfig} onClick={() => void addPHPDatabaseDriver('pdo_mysql')}>Dodaj PDO MySQL</button></div>}
    </div>}
    {needsPHPPostgreSQLDriver && <div className="validation-box">
      <strong>Wybrano PostgreSQL na hoście, ale kontener PHP nie posiada sterownika PostgreSQL.</strong>
      {!readOnly && <div className="actions"><button type="button" className="secondary" disabled={busy !== '' || !runtimeConfig} onClick={() => void addPHPDatabaseDriver('pgsql')}>Dodaj PostgreSQL do PHP</button></div>}
    </div>}

    {!readOnly && <div className="actions">
      {draft.mode === 'managed' && !managedExists
        ? <button type="button" disabled={busy !== ''} onClick={() => void provisionManaged()}>{busy === 'provision' ? 'Tworzenie bazy…' : 'Utwórz bazę i połącz z aplikacją'}</button>
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
