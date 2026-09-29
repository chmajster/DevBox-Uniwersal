import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { request } from '../api/client'
import type { DatabaseRecord, DatabaseUser, MySQLPluginStatus, PostgreSQLPluginStatus } from '../api/types'

type EngineSummaryProps = {
  title: string
  description: string
  engine: 'mysql' | 'postgresql'
  installed: boolean
  running: boolean
  container?: string
  image?: string
  host?: string
  port?: number
  network?: string
  databases: number
  users: number
}

function EngineSummaryTable(props: EngineSummaryProps) {
  const endpoint = props.host && props.port ? `${props.host}:${props.port}` : '—'
  return <section className="panel">
    <div className="section-heading">
      <div>
        <h2>{props.title}</h2>
        <p className="muted">{props.description}</p>
      </div>
      <span className="status-chip" data-ok={props.running ? 'true' : 'false'}>
        {!props.installed ? 'Niezainstalowany' : props.running ? 'Działa' : 'Zatrzymany'}
      </span>
    </div>

    <div className="table-scroll">
      <table>
        <thead>
          <tr>
            <th>Silnik</th>
            <th>Kontener</th>
            <th>Obraz</th>
            <th>Adres dla aplikacji</th>
            <th>Sieć Docker</th>
            <th>Bazy</th>
            <th>Użytkownicy</th>
            <th>Akcje</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>{props.title}</strong></td>
            <td><code>{props.installed ? props.container || '—' : '—'}</code></td>
            <td><code>{props.installed ? props.image || '—' : '—'}</code></td>
            <td><code>{props.installed ? endpoint : '—'}</code></td>
            <td><code>{props.installed ? props.network || '—' : '—'}</code></td>
            <td><span className="badge badge-muted">{props.databases}</span></td>
            <td><span className="badge badge-muted">{props.users}</span></td>
            <td className="actions">
              {props.installed
                ? <Link className="button-link" to={`/databases/${props.engine}`}>Zarządzaj</Link>
                : <Link className="button-link secondary" to="/plugins">Zainstaluj w Pluginach</Link>}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
}

export function DatabasesPage() {
  const [databases, setDatabases] = useState<DatabaseRecord[]>([])
  const [users, setUsers] = useState<DatabaseUser[]>([])
  const [mysql, setMySQL] = useState<MySQLPluginStatus | null>(null)
  const [postgresql, setPostgreSQL] = useState<PostgreSQLPluginStatus | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    const [databaseItems, databaseUsers, mysqlStatus, postgreSQLStatus] = await Promise.all([
      request<DatabaseRecord[]>('/databases'),
      request<DatabaseUser[]>('/database-users'),
      request<MySQLPluginStatus>('/plugins/mysql/status'),
      request<PostgreSQLPluginStatus>('/plugins/postgresql/status'),
    ])
    setDatabases(databaseItems ?? [])
    setUsers(databaseUsers ?? [])
    setMySQL(mysqlStatus)
    setPostgreSQL(postgreSQLStatus)
  }, [])

  useEffect(() => {
    load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się pobrać stanu serwerów baz danych'))
  }, [load])

  const counts = useMemo(() => {
    const mysqlDatabaseIds = new Set(databases.filter((item) => ['mysql', 'mariadb'].includes(item.engine.toLowerCase())).map((item) => item.id))
    const postgreSQLDatabaseIds = new Set(databases.filter((item) => ['postgresql', 'postgres'].includes(item.engine.toLowerCase())).map((item) => item.id))
    return {
      mysqlDatabases: mysqlDatabaseIds.size,
      postgreSQLDatabases: postgreSQLDatabaseIds.size,
      mysqlUsers: users.filter((item) => item.engine === 'mysql' || item.engine === 'mariadb').length,
      postgreSQLUsers: users.filter((item) => item.engine === 'postgresql' || item.engine === 'postgres').length,
    }
  }, [databases, users])

  return <>
    <div className="page-heading">
      <div>
        <h1>Bazy danych</h1>
        <p className="muted">Centralne zarządzanie serwerami SQL, bazami, użytkownikami, uprawnieniami i backupami.</p>
      </div>
      <button type="button" className="secondary" onClick={() => load().catch((cause: unknown) => setError(cause instanceof Error ? cause.message : 'Nie udało się odświeżyć danych'))}>Odśwież</button>
    </div>

    {error && <div className="error-banner">{error}</div>}

    <EngineSummaryTable
      title="MySQL / MariaDB"
      description="Zarządzany serwer aplikacyjny MySQL/MariaDB. Jeden serwer może obsługiwać wiele baz i wielu użytkowników."
      engine="mysql"
      installed={Boolean(mysql?.installed)}
      running={Boolean(mysql?.running)}
      container={mysql?.container_name}
      image={mysql?.image}
      host={mysql?.container_host}
      port={mysql?.port}
      network={mysql?.network}
      databases={counts.mysqlDatabases}
      users={counts.mysqlUsers}
    />

    <EngineSummaryTable
      title="PostgreSQL"
      description="Zarządzany serwer PostgreSQL w Dockerze z osobnymi bazami, rolami, grantami oraz backup/restore."
      engine="postgresql"
      installed={Boolean(postgresql?.installed)}
      running={Boolean(postgresql?.running)}
      container={postgresql?.container_name}
      image={postgresql?.image}
      host={postgresql?.container_host}
      port={postgresql?.port}
      network={postgresql?.network}
      databases={counts.postgreSQLDatabases}
      users={counts.postgreSQLUsers}
    />
  </>
}
