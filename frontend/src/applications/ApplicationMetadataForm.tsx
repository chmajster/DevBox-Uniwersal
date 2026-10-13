import { useState, type FormEvent } from 'react'
import { request } from '../api/client'
import { message, type ApplicationDetail } from './model'

export function ApplicationMetadataForm({ app, disabled, onSaved }: {
  app: ApplicationDetail
  disabled: boolean
  onSaved(): void
}) {
  const [name, setName] = useState(app.name)
  const [description, setDescription] = useState(app.description)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (disabled || saving || !name.trim()) return
    setSaving(true); setError(''); setSaved(false)
    try {
      const updated = await request<ApplicationDetail>(`/applications/${encodeURIComponent(app.id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ name: name.trim(), description: description.trim() }),
      })
      setName(updated.name); setDescription(updated.description); setSaved(true)
      onSaved()
    } catch (cause) { setError(message(cause)) } finally { setSaving(false) }
  }

  return <form onSubmit={(event) => void submit(event)}>
    <fieldset className="acp-card" disabled={disabled || saving}>
      <legend>Nazwa i opis aplikacji</legend>
      <div className="acp-fields">
        <label>Nazwa<input name="name" required maxLength={120} autoComplete="off" value={name} onChange={(event) => { setName(event.target.value); setSaved(false) }} /></label>
        <label>Opis<input name="description" value={description} onChange={(event) => { setDescription(event.target.value); setSaved(false) }} /></label>
      </div>
      <p>Zmiana nazwy lub opisu jest zapisywana od razu i nie wymaga ponownego wdrożenia.</p>
      {error && <p role="alert" className="error-banner">{error}</p>}
      {saved && <p role="status">Nazwa i opis zapisane.</p>}
      <button type="submit" disabled={!name.trim()}>{saving ? 'Zapisywanie…' : 'Zapisz nazwę i opis'}</button>
    </fieldset>
  </form>
}
