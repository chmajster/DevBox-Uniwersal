import { useEffect, useState } from 'react'
import { request } from '../api/client'
import type { Job } from '../api/types'

export interface DatabaseChoice { mode: 'none' | 'existing' | 'create'; database_id: string; user_id: string; engine: string; name: string; username: string; privileges: string }
export const emptyDatabaseChoice: DatabaseChoice = { mode: 'none', database_id: '', user_id: '', engine: 'postgresql', name: '', username: '', privileges: 'SELECT, INSERT, UPDATE, DELETE, CREATE' }
interface Database { id: string; name: string; engine: string; status: string }
interface Account { id: string; username: string; databases: { database_id: string }[] }
interface Server { engine: string; container: string; running: boolean; installed: boolean; error?: string }

export function DatabaseConfiguration({ value, onChange }: { value: DatabaseChoice; onChange(value: DatabaseChoice): void }) {
  const [databases, setDatabases] = useState<Database[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])
  const [servers, setServers] = useState<Server[]>([])
  const [error, setError] = useState('')
  useEffect(() => { let active = true; Promise.all([request<Database[]>('/databases'), request<Account[]>('/database-users'), request<Server[]>('/database-servers')]).then(([dbs, users, instances]) => { if (active) { setDatabases(dbs); setAccounts(users); setServers(instances) } }).catch((e) => { if (active) setError(String(e)) }); return () => { active = false } }, [])
  const change = (patch: Partial<DatabaseChoice>) => onChange({ ...value, ...patch })
  return <div className="acp-fields"><label>Baza danych<select value={value.mode} onChange={(e) => change({ mode: e.target.value as DatabaseChoice['mode'] })}><option value="none">Bez bazy danych</option><option value="existing">Użyj istniejącej bazy</option><option value="create">Utwórz nową bazę i konto</option></select></label>
    {error && <p role="alert">{error}</p>}
    {value.mode === 'existing' && <><label>Baza<select required value={value.database_id} onChange={(e) => change({ database_id: e.target.value, user_id: '' })}><option value="">Wybierz bazę</option>{databases.filter((db) => db.status === 'ready').map((db) => <option key={db.id} value={db.id}>{db.name} · {db.engine}</option>)}</select></label><label>Konto SQL<select value={value.user_id} onChange={(e) => change({ user_id: e.target.value })}><option value="">Pierwsze konto z uprawnieniami</option>{accounts.filter((user) => user.databases.some((grant) => grant.database_id === value.database_id)).map((user) => <option key={user.id} value={user.id}>{user.username}</option>)}<option value="new">Utwórz nowe konto</option></select></label></>}
    {value.mode !== 'none' && <p>Hasło jest generowane i przechowywane w SecretStore. Po wdrożeniu przetestuj uwierzytelnienie i SELECT 1 z kontenera aplikacji.</p>}
    {value.mode === 'create' && <><label>Serwer<select value={value.engine} onChange={(e) => change({ engine: e.target.value })}><option value="postgresql">PostgreSQL</option><option value="mysql">MySQL</option><option value="mariadb">MariaDB</option></select></label><p>{servers.find((server) => server.engine === value.engine)?.running ? 'Serwer działa' : 'Serwer zostanie przygotowany i uruchomiony automatycznie'} · dane w trwałym wolumenie</p><label>Nazwa bazy<input required pattern="[A-Za-z0-9_]+" value={value.name} onChange={(e) => change({ name: e.target.value })} /></label></>}
    {(value.mode === 'create' || (value.mode === 'existing' && value.user_id === 'new')) && <><label>Nowe konto<input pattern="[A-Za-z0-9_]+" value={value.username} onChange={(e) => change({ username: e.target.value })} placeholder="Automatycznie" /></label><label>Uprawnienia<input value={value.privileges} onChange={(e) => change({ privileges: e.target.value })} /></label></>}
  </div>
}

export async function waitForJob(id: string, onProgress?: (message: string) => void): Promise<Job> {
  const deadline = Date.now() + 15 * 60 * 1000
  while (Date.now() < deadline) {
    const job = await request<Job>(`/jobs/${encodeURIComponent(id)}`)
    if (job.status === 'succeeded' || job.status === 'success' || job.status === 'completed') return job
    if (job.status === 'failed' || job.status === 'cancelled') throw new Error(job.error || `Zadanie ${job.status}`)
    onProgress?.(`${job.type}: ${job.status}`)
    await new Promise((resolve) => window.setTimeout(resolve, 1000))
  }
  throw new Error('Zadanie nadal trwa. Sprawdź postęp w zakładce Zadania.')
}

export async function applyDatabaseChoice(applicationID: string, choice: DatabaseChoice, progress?: (message: string) => void) {
  const base = `/applications/${encodeURIComponent(applicationID)}/database-binding`
  if (choice.mode === 'existing' && choice.user_id !== 'new') await request(base, { method: 'PUT', body: JSON.stringify({ database_id: choice.database_id, user_id: choice.user_id }) })
  if (choice.mode === 'create' || (choice.mode === 'existing' && choice.user_id === 'new')) { const job = await request<Job>(`${base}/provision`, { method: 'POST', body: JSON.stringify({ engine: choice.engine, name: choice.name, username: choice.username, ...(choice.mode === 'existing' ? { database_id: choice.database_id } : {}), privileges: choice.privileges.split(',').map((p) => p.trim()).filter(Boolean) }) }); await waitForJob(job.id, progress) }
}
