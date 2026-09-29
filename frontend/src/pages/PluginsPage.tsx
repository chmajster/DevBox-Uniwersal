import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { DockerComposePluginStatus, Job, MySQLPluginStatus, PHPMyAdminStatus, PostgreSQLPluginStatus } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'

type PHPMyAdminAction = 'install' | 'start' | 'stop' | 'restart'

export function PluginsPage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const canInstallSystemPackages = user?.role === 'admin'
  const [dockerCompose, setDockerCompose] = useState<DockerComposePluginStatus | null>(null)
  const [mysql, setMySQL] = useState<MySQLPluginStatus | null>(null)
  const [postgresql, setPostgreSQL] = useState<PostgreSQLPluginStatus | null>(null)
  const [phpMyAdmin, setPHPMyAdmin] = useState<PHPMyAdminStatus | null>(null)
  const [busyAction, setBusyAction] = useState<PHPMyAdminAction | 'docker-compose-install' | 'mysql-install' | 'postgresql-install' | null>(null)
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

  async function installMySQL() {
    setBusyAction('mysql-install')
    setError('')
    setMessage('')
    try {
      const job = await request<Job>('/plugins/mysql/install', { method: 'POST' })
      setMessage(`Instalacja serwera MySQL/MariaDB w Dockerze została dodana do kolejki jako zadanie ${job.id.slice(0, 12)}.`)
      await waitForJob(job.id)
      await load()
      setMessage('Serwer MySQL/MariaDB działa w Dockerze i jest dostępny dla kontenerów aplikacji.')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Instalacja MySQL/MariaDB nie powiodła się')
      await load().catch(() => undefined)
    } finally {
      setBusyAction(null)
    }
  }

  async function installPostgreSQL() {
    setBusyAction('postgresql-install')
    setError('')
    setMessage('')
    try {
      const job = await request<Job>('/plugins/postgresql/install', { method: 'POST' })
      setMessage(`Instalacja serwera PostgreSQL w Dockerze została dodana do kolejki jako zadanie ${job.id.slice(0, 12)}.`)
      await waitForJob(job.id)
      await load()
      setMessage('Serwer PostgreSQL działa w Dockerze i jest dostępny dla kontenerów aplikacji.')
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Instalacja PostgreSQL nie powiodła się')
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

    <section className="panel phpmyadmin-panel">
      <div>
        <div className="actions">
          <Icon name="database" size={24} />
          <div>
            <h2>MySQL / MariaDB</h2>
            <p className="muted">Trwały serwer baz danych uruchamiany w osobnym kontenerze Docker. Jeden serwer może przechowywać wiele baz i obsługiwać wiele aplikacji.</p>
          </div>
        </div>
        <p className="muted small">
          MySQLi i PDO MySQL są sterownikami PHP do tego serwera. Kontenery aplikacji DevBox łączą się z nim przez wspólną sieć Docker.
        </p>
        {mysql?.message && <p className="muted small">{mysql.message}</p>}
      </div>

      <div className="phpmyadmin-status database-plugin-status">
        <div className="actions">
          <span className="status-chip" data-ok={mysql?.installed ? 'true' : 'false'}>
            {mysql?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
          </span>
          {mysql?.installed && <span className="status-chip" data-ok={mysql.running ? 'true' : 'false'}>
            {mysql.running ? 'Działa' : 'Nie odpowiada'}
          </span>}
        </div>

        <div className="database-plugin-meta">
          <div><span>Kontener</span><strong><code>{mysql?.container_name ?? 'devbox-mysql'}</code></strong></div>
          <div><span>Obraz</span><strong><code>{mysql?.image ?? 'mysql:8.4'}</code></strong></div>
          <div><span>Adres dla aplikacji</span><strong><code>{mysql?.container_host ?? 'devbox-mysql'}:{mysql?.port ?? 3306}</code></strong></div>
          <div><span>Sieć Docker</span><strong><code>{mysql?.network ?? 'devbox-apps'}</code></strong></div>
          <div><span>Wolumen danych</span><strong><code>{mysql?.volume ?? 'devbox-mysql-data'}</code></strong></div>
          <div><span>Bazy / aplikacje</span><strong>Wiele / wiele</strong></div>
          <div><span>PHP</span><strong>mysqli / PDO MySQL</strong></div>
        </div>

        <div className="actions">
          {!mysql?.installed && canInstallSystemPackages && mysql?.installable && (
            <button type="button" onClick={() => void installMySQL()} disabled={busy}>
              {busyAction === 'mysql-install' ? 'Instalowanie…' : 'Zainstaluj MySQL / MariaDB'}
            </button>
          )}
          {!mysql?.installed && !canInstallSystemPackages && (
            <span className="muted small">Instalacja serwera wymaga roli administratora DevBox.</span>
          )}
          {!mysql?.installed && canInstallSystemPackages && mysql && !mysql.installable && (
            <span className="muted small">{mysql.message || 'Instalacja z panelu jest obecnie niedostępna.'}</span>
          )}
        </div>
      </div>
    </section>

    <section className="panel phpmyadmin-panel">
      <div>
        <div className="actions">
          <Icon name="database" size={24} />
          <div>
            <h2>PostgreSQL</h2>
            <p className="muted">Trwały serwer PostgreSQL uruchamiany w osobnym kontenerze Docker. Jeden serwer może przechowywać wiele baz i obsługiwać wiele aplikacji.</p>
          </div>
        </div>
        <p className="muted small">
          Kontenery aplikacji DevBox łączą się z nim przez wspólną sieć Docker. Aplikacje PHP używają sterownika <code>pgsql</code> lub <code>PDO PostgreSQL</code>.
        </p>
        {postgresql?.message && <p className="muted small">{postgresql.message}</p>}
      </div>

      <div className="phpmyadmin-status database-plugin-status">
        <div className="actions">
          <span className="status-chip" data-ok={postgresql?.installed ? 'true' : 'false'}>
            {postgresql?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
          </span>
          {postgresql?.installed && <span className="status-chip" data-ok={postgresql.running ? 'true' : 'false'}>
            {postgresql.running ? 'Działa' : 'Nie odpowiada'}
          </span>}
        </div>

        <div className="database-plugin-meta">
          <div><span>Kontener</span><strong><code>{postgresql?.container_name ?? 'devbox-postgresql'}</code></strong></div>
          <div><span>Obraz</span><strong><code>{postgresql?.image ?? 'postgres:17'}</code></strong></div>
          <div><span>Adres dla aplikacji</span><strong><code>{postgresql?.container_host ?? 'devbox-postgresql'}:{postgresql?.port ?? 5432}</code></strong></div>
          <div><span>Sieć Docker</span><strong><code>{postgresql?.network ?? 'devbox-apps'}</code></strong></div>
          <div><span>Wolumen danych</span><strong><code>{postgresql?.volume ?? 'devbox-postgresql-data'}</code></strong></div>
          <div><span>Bazy / aplikacje</span><strong>Wiele / wiele</strong></div>
          <div><span>PHP</span><strong>pgsql / PDO PostgreSQL</strong></div>
        </div>

        <div className="actions">
          {!postgresql?.installed && canInstallSystemPackages && postgresql?.installable && (
            <button type="button" onClick={() => void installPostgreSQL()} disabled={busy}>
              {busyAction === 'postgresql-install' ? 'Instalowanie…' : 'Zainstaluj PostgreSQL'}
            </button>
          )}
          {!postgresql?.installed && !canInstallSystemPackages && (
            <span className="muted small">Instalacja serwera wymaga roli administratora DevBox.</span>
          )}
          {!postgresql?.installed && canInstallSystemPackages && postgresql && !postgresql.installable && (
            <span className="muted small">{postgresql.message || 'Instalacja z panelu jest obecnie niedostępna.'}</span>
          )}
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
          phpMyAdmin działa na tej samej sieci Docker co serwer MySQL i aplikacje. Domyślny serwer to <code>{mysql?.container_host ?? 'devbox-mysql'}:{mysql?.port ?? 3306}</code>.
          Tryb arbitrary server nadal pozwala wskazać inny serwer MySQL/MariaDB.
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
