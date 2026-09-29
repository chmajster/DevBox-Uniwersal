import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { DockerComposePluginStatus, Job, MySQLPluginStatus, PHPMyAdminStatus, PostgreSQLPluginStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'

type PHPMyAdminAction = 'install' | 'start' | 'stop' | 'restart'
type SQLInstallEngine = 'mysql' | 'postgresql'

export function PluginsPage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const canInstallSystemPackages = user?.role === 'admin'
  const [dockerCompose, setDockerCompose] = useState<DockerComposePluginStatus | null>(null)
  const [mysql, setMySQL] = useState<MySQLPluginStatus | null>(null)
  const [postgresql, setPostgreSQL] = useState<PostgreSQLPluginStatus | null>(null)
  const [phpMyAdmin, setPHPMyAdmin] = useState<PHPMyAdminStatus | null>(null)
  const [selectedSQLEngines, setSelectedSQLEngines] = useState<SQLInstallEngine[]>([])
  const [busyAction, setBusyAction] = useState<PHPMyAdminAction | 'docker-compose-install' | 'sql-install' | null>(null)
  const [installProgress, setInstallProgress] = useState<number | null>(null)
  const [installPhase, setInstallPhase] = useState('')
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    const [dockerComposeStatus, mySQLStatus, postgreSQLStatus, phpMyAdminStatus] = await Promise.all([
      request<DockerComposePluginStatus>('/plugins/docker-compose/status'),
      request<MySQLPluginStatus>('/plugins/mysql/status'),
      request<PostgreSQLPluginStatus>('/plugins/postgresql/status'),
      request<PHPMyAdminStatus>('/phpmyadmin/status'),
    ])
    setDockerCompose(dockerComposeStatus)
    setMySQL(mySQLStatus)
    setPostgreSQL(postgreSQLStatus)
    setPHPMyAdmin(phpMyAdminStatus)
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać statusu pluginów'))
  }, [load])

  async function installDockerCompose() {
    setBusyAction('docker-compose-install')
    setError('')
    setMessage('')
    try {
      const status = await request<DockerComposePluginStatus>('/plugins/docker-compose/install', { method: 'POST' })
      setDockerCompose(status)
      setMessage('Docker Compose został zainstalowany i jest gotowy do wdrażania projektów Compose.')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Instalacja Docker Compose nie powiodła się')
      await load().catch(() => undefined)
    } finally {
      setBusyAction(null)
    }
  }

  async function waitForJob(jobId: string): Promise<Job> {
    const deadline = Date.now() + 5 * 60 * 1000
    while (Date.now() < deadline) {
      const job = await request<Job>(`/jobs/${encodeURIComponent(jobId)}`)
      if (job.status === 'succeeded') return job
      if (job.status === 'failed' || job.status === 'cancelled') {
        throw new Error(job.error || `Zadanie ${job.status}.`)
      }
      await new Promise((resolve) => window.setTimeout(resolve, 1000))
    }
    throw new Error('Instalacja serwera SQL nadal trwa. Sprawdź status zadania w zakładce Zadania.')
  }

  function toggleSQLEngine(engine: SQLInstallEngine, checked: boolean) {
    setSelectedSQLEngines((current) => checked
      ? Array.from(new Set([...current, engine]))
      : current.filter((item) => item !== engine))
  }

  async function installSelectedSQL() {
    const engines = selectedSQLEngines.filter((engine) =>
      engine === 'mysql' ? !mysql?.installed : !postgresql?.installed)
    if (engines.length === 0) {
      setError('Wybierz co najmniej jeden niezainstalowany silnik SQL.')
      return
    }

    setBusyAction('sql-install')
    setError('')
    setMessage('')
    try {
      const jobs: Job[] = []
      for (const engine of engines) {
        const endpoint = engine === 'mysql' ? '/plugins/mysql/install' : '/plugins/postgresql/install'
        jobs.push(await request<Job>(endpoint, { method: 'POST' }))
      }
      const labels = engines.map((engine) => engine === 'mysql' ? 'MySQL/MariaDB' : 'PostgreSQL')
      setMessage(`Instalacja ${labels.join(' + ')} została dodana do kolejki.`)
      await Promise.all(jobs.map((job) => waitForJob(job.id)))
      await load()
      setSelectedSQLEngines([])
      setMessage(`${labels.join(' + ')} są gotowe jako hostowe serwery SQL dla aplikacji.`)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Instalacja wybranych serwerów SQL nie powiodła się')
      await load().catch(() => undefined)
    } finally {
      setBusyAction(null)
    }
  }

  async function openPHPMyAdmin() {
    setBusyAction('start')
    setError('')
    setMessage('')
    try {
      const status = await request<PHPMyAdminStatus>('/phpmyadmin/start', { method: 'POST', body: '{}' })
      setPHPMyAdmin(status)
      if (!status.running) throw new Error('phpMyAdmin nie został uruchomiony.')
      if (!status.url) throw new Error('phpMyAdmin nie zwrócił adresu aplikacji.')
      window.open(status.url, '_blank', 'noopener,noreferrer')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się otworzyć phpMyAdmin')
      await load().catch(() => undefined)
    } finally {
      setBusyAction(null)
    }
  }

  async function phpAction(action: PHPMyAdminAction) {
    let installTimer: number | undefined
    let installSucceeded = false

    setBusyAction(action)
    setError('')
    setMessage('')

    if (action === 'install') {
      setInstallProgress(5)
      setInstallPhase('Przygotowywanie instalacji…')
      installTimer = window.setInterval(() => {
        setInstallProgress((current) => {
          const next = Math.min((current ?? 5) + 7, 92)
          if (next < 35) setInstallPhase('Przygotowywanie kontenera phpMyAdmin…')
          else if (next < 75) setInstallPhase('Pobieranie obrazu i uruchamianie kontenera…')
          else setInstallPhase('Weryfikacja stanu usługi…')
          return next
        })
      }, 700)
    }

    try {
      const status = await request<PHPMyAdminStatus>(`/phpmyadmin/${action}`, { method: 'POST' })
      setPHPMyAdmin(status)
      installSucceeded = action === 'install'
      const labels: Record<PHPMyAdminAction, string> = {
        install: 'phpMyAdmin został zainstalowany.',
        start: 'phpMyAdmin został uruchomiony.',
        stop: 'phpMyAdmin został zatrzymany.',
        restart: 'phpMyAdmin został zrestartowany.',
      }
      setMessage(labels[action])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Operacja phpMyAdmin nie powiodła się')
      await load().catch(() => undefined)
    } finally {
      if (installTimer !== undefined) window.clearInterval(installTimer)
      setBusyAction(null)

      if (action === 'install') {
        if (installSucceeded) {
          setInstallProgress(100)
          setInstallPhase('phpMyAdmin został zainstalowany.')
          window.setTimeout(() => {
            setInstallProgress(null)
            setInstallPhase('')
          }, 1200)
        } else {
          setInstallProgress(null)
          setInstallPhase('')
        }
      }
    }
  }

  const busy = busyAction !== null

  return <>
    <div className="page-heading">
      <div>
        <h1>Pluginy</h1>
        <p className="muted">Dodatkowe narzędzia rozszerzające DevBox Universal.</p>
      </div>
      <button type="button" className="secondary" onClick={() => load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć statusu'))} disabled={busy}>
        <Icon name="refresh" size={16} /> Odśwież
      </button>
    </div>

    {error && <div className="error-banner">{error}</div>}
    {message && <div className="success-banner">{message}</div>}

    <section className="panel phpmyadmin-panel">
      <div>
        <div className="actions">
          <Icon name="box" size={24} />
          <div>
            <h2>Docker Compose</h2>
            <p className="muted">Wymagany do wdrażania aplikacji zawierających <code>compose.yaml</code> lub <code>docker-compose.yml</code>.</p>
          </div>
        </div>
        <p className="muted small">
          {dockerCompose?.installed
            ? 'Docker Compose został wykryty i może być używany przez deploymenty.'
            : 'Docker Engine działa, ale Docker Compose nie jest dostępny. Deployment projektu z plikiem Compose zakończy się błędem przed uruchomieniem kontenerów.'}
        </p>
      </div>

      <div className="phpmyadmin-status">
        <div className="actions">
          <span className="status-chip" data-ok={dockerCompose?.installed ? 'true' : 'false'}>
            {dockerCompose?.installed ? 'Dostępny' : 'Wymagana instalacja'}
          </span>
          {dockerCompose?.mode && <span className="status-chip" data-ok="true">{dockerCompose.mode === 'plugin' ? 'Compose v2 plugin' : 'docker-compose'}</span>}
        </div>

        {dockerCompose?.version && <p className="muted small">Wersja: <code>{dockerCompose.version}</code></p>}
        {dockerCompose?.path && <p className="muted small">Ścieżka: <code>{dockerCompose.path}</code></p>}
        {dockerCompose?.message && <p className="muted small">{dockerCompose.message}</p>}

        <div className="actions">
          {!dockerCompose?.installed && canInstallSystemPackages && dockerCompose?.installable && (
            <button type="button" onClick={installDockerCompose} disabled={busy}>
              {busyAction === 'docker-compose-install' ? 'Instalowanie…' : 'Zainstaluj Docker Compose'}
            </button>
          )}
          {!dockerCompose?.installed && !canInstallSystemPackages && (
            <span className="muted small">Instalacja pakietu systemowego wymaga roli administratora.</span>
          )}
          {!dockerCompose?.installed && canInstallSystemPackages && dockerCompose && !dockerCompose.installable && (
            <span className="muted small">Instalacja z panelu jest niedostępna, ponieważ privileged helper nie jest skonfigurowany.</span>
          )}
        </div>
      </div>
    </section>

    <section className="panel database-server-plugin">
      <div className="section-heading database-server-plugin-heading">
        <div className="actions">
          <Icon name="database" size={24} />
          <div>
            <h2>Serwery baz danych dla aplikacji</h2>
            <p className="muted">Instalujesz serwer bazodanowy, nie pojedynczą bazę. Jeden serwer może przechowywać wiele baz i obsługiwać wiele aplikacji jednocześnie.</p>
          </div>
        </div>
        <div className="actions">
          <span className="status-chip" data-ok={mysql?.running || postgresql?.running ? 'true' : 'false'}>
            Aktywne: {[mysql?.running, postgresql?.running].filter(Boolean).length}/2
          </span>
        </div>
      </div>

      <div className="database-server-explainer">
        <strong>Model współdzielony</strong>
        <span>1 serwer → wiele baz → wielu użytkowników → wiele aplikacji. Każda aplikacja może korzystać z własnej bazy i własnego użytkownika na tym samym serwerze.</span>
        <span>DevBox Universal przechowuje swój własny stan w SQLite. Serwery poniżej są przeznaczone wyłącznie dla danych aplikacji.</span>
      </div>

      <div className="database-server-grid">
        <article className="database-server-engine" data-installed={mysql?.installed ? 'true' : 'false'}>
          <div className="database-server-engine-header">
            <div>
              <div className="database-server-engine-title">
                {!mysql?.installed && canInstallSystemPackages && mysql?.installable && (
                  <input
                    aria-label="Zaznacz MySQL lub MariaDB do instalacji"
                    type="checkbox"
                    checked={selectedSQLEngines.includes('mysql')}
                    disabled={busy}
                    onChange={(event) => toggleSQLEngine('mysql', event.target.checked)}
                  />
                )}
                <strong>MySQL / MariaDB</strong>
              </div>
              <span className="muted small">Współdzielony serwer SQL dla wielu baz i wielu aplikacji.</span>
            </div>
            <div className="actions">
              <span className="status-chip" data-ok={mysql?.installed ? 'true' : 'false'}>
                {mysql?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
              </span>
              {mysql?.installed && <span className="status-chip" data-ok={mysql.running ? 'true' : 'false'}>
                {mysql.running ? 'Działa' : 'Nie odpowiada'}
              </span>}
            </div>
          </div>

          <div className="database-server-engine-meta">
            <div><span>Typ</span><strong>Serwer bazodanowy</strong></div>
            <div><span>Silnik</span><strong>{mysql?.engine === 'mariadb' ? 'MariaDB' : 'MySQL'}</strong></div>
            <div><span>Port</span><strong><code>{mysql?.port ?? 3306}</code></strong></div>
            <div><span>Adres dla aplikacji</span><strong><code>{mysql?.container_host ?? 'host.docker.internal'}:{mysql?.port ?? 3306}</code></strong></div>
            <div><span>Obsługiwane bazy</span><strong>Wiele</strong></div>
            <div><span>Obsługiwane aplikacje</span><strong>Wiele</strong></div>
            <div><span>PHP</span><strong>mysqli / PDO MySQL</strong></div>
            <div><span>Model</span><strong>1 serwer → N baz</strong></div>
          </div>

          {mysql?.version && <p className="muted small">Wersja: <code>{mysql.version}</code></p>}
          {mysql?.message && <p className="muted small">{mysql.message}</p>}
          {!mysql?.installed && canInstallSystemPackages && mysql && !mysql.installable && (
            <p className="muted small">{mysql.message || 'Instalacja MySQL/MariaDB jest obecnie niedostępna.'}</p>
          )}
        </article>

        <article className="database-server-engine" data-installed={postgresql?.installed ? 'true' : 'false'}>
          <div className="database-server-engine-header">
            <div>
              <div className="database-server-engine-title">
                {!postgresql?.installed && canInstallSystemPackages && postgresql?.installable && (
                  <input
                    aria-label="Zaznacz PostgreSQL do instalacji"
                    type="checkbox"
                    checked={selectedSQLEngines.includes('postgresql')}
                    disabled={busy}
                    onChange={(event) => toggleSQLEngine('postgresql', event.target.checked)}
                  />
                )}
                <strong>PostgreSQL</strong>
              </div>
              <span className="muted small">Współdzielony serwer PostgreSQL dla wielu baz i wielu aplikacji.</span>
            </div>
            <div className="actions">
              <span className="status-chip" data-ok={postgresql?.installed ? 'true' : 'false'}>
                {postgresql?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
              </span>
              {postgresql?.installed && <span className="status-chip" data-ok={postgresql.running ? 'true' : 'false'}>
                {postgresql.running ? 'Działa' : 'Nie odpowiada'}
              </span>}
            </div>
          </div>

          <div className="database-server-engine-meta">
            <div><span>Typ</span><strong>Serwer bazodanowy</strong></div>
            <div><span>Silnik</span><strong>PostgreSQL</strong></div>
            <div><span>Port</span><strong><code>{postgresql?.port ?? 5432}</code></strong></div>
            <div><span>Adres dla aplikacji</span><strong><code>{postgresql?.container_host ?? 'host.docker.internal'}:{postgresql?.port ?? 5432}</code></strong></div>
            <div><span>Obsługiwane bazy</span><strong>Wiele</strong></div>
            <div><span>Obsługiwane aplikacje</span><strong>Wiele</strong></div>
            <div><span>PHP</span><strong>pgsql / PDO PostgreSQL</strong></div>
            <div><span>Model</span><strong>1 serwer → N baz</strong></div>
          </div>

          {postgresql?.version && <p className="muted small">Wersja: <code>{postgresql.version}</code></p>}
          {postgresql?.message && <p className="muted small">{postgresql.message}</p>}
          {!postgresql?.installed && canInstallSystemPackages && postgresql && !postgresql.installable && (
            <p className="muted small">Instalacja PostgreSQL jest obecnie niedostępna.</p>
          )}
        </article>
      </div>

      <div className="database-server-plugin-footer">
        <div>
          <strong>Serwer ≠ baza</strong>
          <p className="muted small">Plugin instaluje i wykrywa silnik serwera. Konkretne bazy, użytkownicy i przypisania aplikacji są osobnymi zasobami i mogą współdzielić ten sam serwer. MySQLi jest sterownikiem PHP do MySQL, a nie osobnym serwerem bazodanowym.</p>
        </div>
        <div className="actions">
          {canInstallSystemPackages && (
            <button
              type="button"
              onClick={() => void installSelectedSQL()}
              disabled={busy || selectedSQLEngines.length === 0}
            >
              {busyAction === 'sql-install' ? 'Instalowanie serwerów…' : 'Zainstaluj zaznaczone serwery'}
            </button>
          )}
          {!canInstallSystemPackages && <span className="muted small">Instalacja serwerów baz danych wymaga roli administratora.</span>}
        </div>
      </div>
    </section>

    <section className="panel phpmyadmin-panel">
      <div>
        <div className="actions">
          <Icon name="database" size={24} />
          <div>
            <h2>phpMyAdmin</h2>
            <p className="muted">Webowy panel do zarządzania MySQL/MariaDB. Uruchamiany jako niezależny kontener Docker.</p>
          </div>
        </div>
        <p className="muted small">Plugin korzysta z istniejącej konfiguracji DevBox i nie jest powiązany z lifecycle pojedynczej aplikacji.</p>
      </div>

      <div className="phpmyadmin-status">
        {installProgress !== null && <div className="phpmyadmin-install-progress" role="status" aria-live="polite">
          <div className="phpmyadmin-install-progress-header">
            <div>
              <strong>{installProgress < 100 ? 'Instalowanie phpMyAdmin' : 'Instalacja zakończona'}</strong>
              <span>{installPhase}</span>
            </div>
            <strong className="phpmyadmin-install-percent">{installProgress}%</strong>
          </div>
          <div
            className="phpmyadmin-progress-track"
            role="progressbar"
            aria-label="Postęp instalacji phpMyAdmin"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={installProgress}
          >
            <div className="phpmyadmin-progress-value" style={{ width: `${installProgress}%` }} />
          </div>
        </div>}

        <div className="actions">
          <span className="status-chip" data-ok={phpMyAdmin?.installed ? 'true' : 'false'}>
            {phpMyAdmin?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
          </span>
          <span className="status-chip" data-ok={phpMyAdmin?.running ? 'true' : 'false'}>
            {busyAction === 'install' ? 'installing' : phpMyAdmin?.running ? 'Uruchomiony' : phpMyAdmin?.state ?? 'Zatrzymany'}
          </span>
          {phpMyAdmin?.installed && <span className="status-chip" data-ok={phpMyAdmin.host_database_access ? 'true' : 'false'}>
            {phpMyAdmin.host_database_access ? 'Host gateway skonfigurowany' : 'Wymaga rekonfiguracji host MySQL'}
          </span>}
          {phpMyAdmin?.running && phpMyAdmin.host_database_access && <span className="status-chip" data-ok={phpMyAdmin.host_database_reachable ? 'true' : 'false'}>
            {phpMyAdmin.host_database_reachable ? 'Host MySQL osiągalny' : 'Host MySQL nieosiągalny'}
          </span>}
        </div>

        <p className="muted small">
          phpMyAdmin ma dostęp do hostowego MySQL/MariaDB przez <code>{phpMyAdmin?.host_database_host ?? 'host.docker.internal'}:{phpMyAdmin?.host_database_port ?? 3306}</code>.
          Na ekranie logowania możesz wybrać serwer hostowy albo wpisać inny serwer dzięki trybowi arbitrary server.
        </p>

        <div className="actions">
          {canMutate && !phpMyAdmin?.installed && (
            <button type="button" onClick={() => phpAction('install')} disabled={busy}>
              {busyAction === 'install' ? 'Instalowanie…' : 'Zainstaluj'}
            </button>
          )}
          {canMutate && phpMyAdmin?.installed && !phpMyAdmin.running && (
            <button type="button" onClick={() => phpAction('start')} disabled={busy}>
              {busyAction === 'start' ? 'Uruchamianie…' : 'Uruchom'}
            </button>
          )}
          {canMutate && phpMyAdmin?.running && (
            <button type="button" className="secondary" onClick={() => phpAction('restart')} disabled={busy}>
              {busyAction === 'restart' ? 'Restartowanie…' : 'Restart'}
            </button>
          )}
          {canMutate && phpMyAdmin?.running && (
            <button type="button" className="secondary" onClick={() => phpAction('stop')} disabled={busy}>
              {busyAction === 'stop' ? 'Zatrzymywanie…' : 'Zatrzymaj'}
            </button>
          )}
          {phpMyAdmin?.running && phpMyAdmin.url && (
            <button type="button" onClick={() => void openPHPMyAdmin()} disabled={busy}>
              {busyAction === 'start' ? 'Sprawdzanie połączenia…' : 'Otwórz phpMyAdmin'}
            </button>
          )}
        </div>
      </div>
    </section>
  </>
}
