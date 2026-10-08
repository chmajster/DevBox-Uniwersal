import { useState } from 'react'
import { encodeEnvironment, parseEnv, type EnvironmentRow } from './environment'

export function EnvironmentEditor({ value = {} }: { value?: Record<string, unknown> }) {
  const [rows, setRows] = useState<EnvironmentRow[]>(Object.entries(value).map(([name, value]) => ({ name, value: String(value), secret: false })))
  const [error, setError] = useState('')
  let encoded = { publicValues: {}, secrets: {} }
  try { encoded = encodeEnvironment(rows) } catch { /* Form submission validates rows. */ }
  function update(index: number, patch: Partial<EnvironmentRow>) { setRows(rows.map((row, i) => i === index ? { ...row, ...patch } : row)) }
  return <section className="acp-wide"><h3>Environment variables</h3>
    {rows.map((row, index) => <div className="acp-env-row" key={index}>
      <input aria-label={`Nazwa zmiennej ${index + 1}`} required pattern="[A-Za-z_][A-Za-z0-9_]*" value={row.name} onChange={(event) => update(index, { name: event.target.value })} placeholder="KEY" />
      <span>=</span><input aria-label={`Wartość zmiennej ${index + 1}`} type={row.secret ? 'password' : 'text'} autoComplete="off" value={row.value} onChange={(event) => update(index, { value: event.target.value })} placeholder="VALUE" />
      <label className="acp-check"><input type="checkbox" checked={row.secret} onChange={(event) => update(index, { secret: event.target.checked })} />Sekret</label>
      <button className="secondary-button" type="button" onClick={() => setRows(rows.filter((_, i) => i !== index))}>Usuń</button>
    </div>)}
    <input type="hidden" name="environment_json" value={JSON.stringify(encoded.publicValues)} />
    <input type="hidden" name="secret_environment" value={JSON.stringify(encoded.secrets)} />
    <input type="hidden" name="environment_rows" value={JSON.stringify(rows)} />
    <div className="acp-actions"><button className="secondary-button" type="button" onClick={() => setRows([...rows, { name: '', value: '', secret: false }])}>Dodaj zmienną</button>
      <label>Import .env<input type="file" accept=".env,text/plain" onChange={async (event) => {
        const file = event.target.files?.[0]; if (!file) return
        try { if (file.size > 65536) throw new Error('Plik .env może mieć maksymalnie 64 KB.'); setRows(parseEnv(await file.text())); setError('') } catch (error) { setError(String(error)) }
      }} /></label></div>
    {error && <p role="alert">{error}</p>}<small>Sekrety są maskowane i zapisywane w szyfrowanym SecretStore.</small>
  </section>
}
