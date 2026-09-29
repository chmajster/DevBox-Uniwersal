import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { DockerComposePluginStatus, PHPFPMStatus, PHPMyAdminStatus, PostgreSQLPluginStatus, MySQLPluginStatus, Job } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { Icon } from '../components/Icon'
import { DurableJobNotice, readPendingJob, storePendingJob } from '../components/DurableJobNotice'

type PHPMyAdminAction = 'install' | 'start' | 'stop' | 'restart'
const pendingKey = 'devbox.plugin-job'
export function PluginsPage() {
  const { user } = useAuth()
  const canMutate = user?.role !== 'viewer'
  const canInstallSystemPackages = user?.role === 'admin'
  const [dockerCompose, setDockerCompose] = useState<DockerComposePluginStatus | null>(null)
  const [phpFPM, setPHPFPM] = useState<PHPFPMStatus | null>(null)
  const [postgresql, setPostgreSQL] = useState<PostgreSQLPluginStatus | null>(null)
  const [mysql, setMySQL] = useState<MySQLPluginStatus | null>(null)
  const [phpMyAdmin, setPHPMyAdmin] = useState<PHPMyAdminStatus | null>(null)
  const [jobId, setJobId] = useState(() => readPendingJob(pendingKey))
  const [busyAction, setBusyAction] = useState<string | null>(() => readPendingJob(pendingKey) ? 'resume' : null)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const load = useCallback(async () => {
    const [compose, php, postgres, pma, hostMySQL] = await Promise.all([
      request<DockerComposePluginStatus>('/plugins/docker-compose/status'),
      request<PHPFPMStatus>('/plugins/php-fpm/status'),
      request<PostgreSQLPluginStatus>('/plugins/postgresql/status'),
      request<PHPMyAdminStatus>('/phpmyadmin/status'),
      request<MySQLPluginStatus>('/plugins/mysql/status'),
    ])
    setDockerCompose(compose); setPHPFPM(php); setPostgreSQL(postgres); setPHPMyAdmin(pma); setMySQL(hostMySQL)
  }, [])
  useEffect(() => { void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać statusu pluginów')) }, [load])
  async function enqueue(path: string, action: string) {
    setBusyAction(action); setError(''); setMessage('')
    try {
      const job = await request<Job>(path, { method: 'POST', body: '{}' })
      storePendingJob(pendingKey, job.id); setJobId(job.id)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Nie udało się utworzyć zadania'); setBusyAction(null)
    }
  }
  function finishJob(job: Job) {
    storePendingJob(pendingKey, ''); setBusyAction(null)
    if (job.status === 'succeeded') setMessage('Operacja zakończona. Status komponentów został odświeżony.')
    else setError(job.error || `Operacja: ${job.status}`)
    void load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Odczyt statusu nie powiódł się'))
  }
  const installDockerCompose = () => enqueue('/plugins/docker-compose/install', 'docker-compose-install')
  const installPHPFPM = () => enqueue('/plugins/php-fpm/install', 'php-fpm-install')
  const installPostgreSQL = () => enqueue('/plugins/postgresql/install', 'postgresql-install')
  const phpAction = (action: PHPMyAdminAction) => enqueue(`/phpmyadmin/${action}`, action)
  const openPHPMyAdmin = () => phpAction('start')
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
    {jobId && <DurableJobNotice jobId={jobId} onComplete={finishJob} />}
    {phpMyAdmin?.running && <p><a href={phpMyAdmin.url} target="_blank" rel="noreferrer">Przejdź do działającego phpMyAdmin</a></p>}

    <section className="panel phpmyadmin-panel">
      <div><h2>MySQL / MariaDB na hoście</h2><p className="muted">Osobny, opcjonalny serwer. Instalacja nie zmienia bind-address ani grantów użytkowników i nie zastępuje kontenera devbox-mysql.</p></div>
      <div className="phpmyadmin-status">
        <p>{mysql?.installed ? `${mysql.engine ?? 'MySQL'} — ${mysql.running ? 'odpowiada' : 'nie odpowiada'}` : 'Niezainstalowany'}</p>
        {mysql?.version && <p>Wersja: {mysql.version}</p>}
        {mysql && <p>Z kontenera: <code>host.docker.internal:{mysql.port}</code></p>}
        {mysql?.message && <p className="muted">{mysql.message}</p>}
        {canInstallSystemPackages && !mysql?.installed && mysql?.installable && <button type="button" disabled={busy} onClick={() => void enqueue('/plugins/mysql/install', 'mysql-install')}>Zainstaluj MySQL / MariaDB</button>}
      </div>
    </section>

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
          <Icon name="cpu" size={24} />
          <div>
            <h2>PHP-FPM</h2>
            <p className="muted">Opcjonalny PHP-FPM na hoście. Aplikacje zarządzane przez DevBox korzystają z własnego runtime w kontenerze.</p>
          </div>
        </div>
        <p className="muted small">
          {phpFPM?.installed
            ? 'PHP-FPM został wykryty w systemie.'
            : 'PHP-FPM nie jest zainstalowany na hoście. Nie blokuje to aplikacji PHP uruchamianych w kontenerze.'}
        </p>
      </div>

      <div className="phpmyadmin-status">
        <div className="actions">
          <span className="status-chip" data-ok={phpFPM?.installed ? 'true' : 'false'}>
            {phpFPM?.installed ? 'Zainstalowany' : 'Wymagana instalacja'}
          </span>
          {phpFPM?.version && <span className="status-chip" data-ok="true">{phpFPM.version}</span>}
        </div>

        {phpFPM?.path && <p className="muted small">Ścieżka: <code>{phpFPM.path}</code></p>}
        {phpFPM?.message && <p className="muted small">{phpFPM.message}</p>}

        <div className="actions">
          {!phpFPM?.installed && canInstallSystemPackages && phpFPM?.installable && (
            <button type="button" onClick={installPHPFPM} disabled={busy}>
              {busyAction === 'php-fpm-install' ? 'Instalowanie…' : 'Zainstaluj PHP-FPM'}
            </button>
          )}
          {!phpFPM?.installed && !canInstallSystemPackages && (
            <span className="muted small">Instalacja pakietu systemowego wymaga roli administratora.</span>
          )}
          {!phpFPM?.installed && canInstallSystemPackages && phpFPM && !phpFPM.installable && (
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
            <h2>PostgreSQL</h2>
            <p className="muted">Opcjonalny lokalny serwer PostgreSQL instalowany przez systemowy manager pakietów.</p>
          </div>
        </div>
        <p className="muted small">
          {postgresql?.installed
            ? 'PostgreSQL jest zainstalowany. Status poniżej pokazuje, czy lokalny serwer odpowiada.'
            : 'PostgreSQL nie jest wymagany przez DevBox. Możesz go doinstalować, jeżeli projekty potrzebują lokalnego serwera PostgreSQL.'}
        </p>
      </div>

      <div className="phpmyadmin-status">
        <div className="actions">
          <span className="status-chip" data-ok={postgresql?.installed ? 'true' : 'false'}>
            {postgresql?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
          </span>
          {postgresql?.installed && <span className="status-chip" data-ok={postgresql.running ? 'true' : 'false'}>
            {postgresql.running ? 'Uruchomiony' : 'Nie odpowiada'}
          </span>}
        </div>

        {postgresql?.version && <p className="muted small">Wersja: <code>{postgresql.version}</code></p>}
        {postgresql?.path && <p className="muted small">Klient: <code>{postgresql.path}</code></p>}
        {postgresql?.host && postgresql?.port && <p className="muted small">Adres lokalny: <code>{postgresql.host}:{postgresql.port}</code></p>}
        {postgresql?.message && <p className="muted small">{postgresql.message}</p>}

        <div className="actions">
          {!postgresql?.installed && canInstallSystemPackages && postgresql?.installable && (
            <button type="button" onClick={installPostgreSQL} disabled={busy}>
              {busyAction === 'postgresql-install' ? 'Instalowanie…' : 'Zainstaluj PostgreSQL'}
            </button>
          )}
          {!postgresql?.installed && !canInstallSystemPackages && (
            <span className="muted small">Instalacja PostgreSQL wymaga roli administratora.</span>
          )}
          {!postgresql?.installed && canInstallSystemPackages && postgresql && !postgresql.installable && (
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
            <h2>phpMyAdmin</h2>
            <p className="muted">Webowy panel do zarządzania MySQL/MariaDB. Uruchamiany jako niezależny kontener Docker.</p>
          </div>
        </div>
        <p className="muted small">Plugin korzysta z istniejącej konfiguracji DevBox i nie jest powiązany z lifecycle pojedynczej aplikacji.</p>
      </div>

      <div className="phpmyadmin-status">

        <div className="actions">
          <span className="status-chip" data-ok={phpMyAdmin?.installed ? 'true' : 'false'}>
            {phpMyAdmin?.installed ? 'Zainstalowany' : 'Niezainstalowany'}
          </span>
          <span className="status-chip" data-ok={phpMyAdmin?.running ? 'true' : 'false'}>
            {busyAction === 'install' ? 'installing' : phpMyAdmin?.running ? 'Uruchomiony' : phpMyAdmin?.state ?? 'Zatrzymany'}
          </span>
        </div>

        {phpMyAdmin?.installed && <p className="muted small">Dostęp host-gateway: {phpMyAdmin.host_database_access ? 'skonfigurowany' : 'wymaga rekonfiguracji'}. TCP do {phpMyAdmin.host_database_host ?? 'host.docker.internal'}:{phpMyAdmin.host_database_port ?? 3306}: {phpMyAdmin.host_database_reachable ? 'osiągalny' : 'nieosiągalny lub kontener zatrzymany'}. Ten test nie sprawdza loginu ani hasła.</p>}
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
