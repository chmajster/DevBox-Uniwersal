import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { request } from '../api/client'
import type { CentralCredential } from '../api/types'

export function CredentialsPage() {
  const [items,setItems]=useState<CentralCredential[]>([])
  const [name,setName]=useState('')
  const [kind,setKind]=useState<'token'|'ssh_key'>('token')
  const [secret,setSecret]=useState('')
  const [editing,setEditing]=useState<CentralCredential|null>(null)
  const [replacement,setReplacement]=useState('')
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  const [message,setMessage]=useState('')

  const load=useCallback(async()=>{
    const data=await request<CentralCredential[]>('/credentials')
    setItems(data??[])
  },[])

  useEffect(()=>{load().catch(c=>setError(c instanceof Error?c.message:'Nie udało się pobrać poświadczeń'))},[load])

  async function create(event:FormEvent){
    event.preventDefault(); setBusy(true); setError(''); setMessage('')
    try{
      await request<CentralCredential>('/credentials',{method:'POST',body:JSON.stringify({name,kind,secret})})
      setName(''); setSecret(''); setMessage('Poświadczenie zapisane centralnie. Można je teraz wybierać w aplikacjach.')
      await load()
    }catch(c){setError(c instanceof Error?c.message:'Nie udało się zapisać poświadczenia')}
    finally{setBusy(false)}
  }

  async function rotate(event:FormEvent){
    event.preventDefault()
    if(!editing)return
    setBusy(true); setError(''); setMessage('')
    try{
      await request<CentralCredential>(`/credentials/${editing.id}`,{method:'PATCH',body:JSON.stringify({secret:replacement})})
      setEditing(null); setReplacement(''); setMessage('Sekret został zastąpiony.')
      await load()
    }catch(c){setError(c instanceof Error?c.message:'Nie udało się zaktualizować sekretu')}
    finally{setBusy(false)}
  }

  async function remove(item:CentralCredential){
    if(!window.confirm(`Usunąć poświadczenie „${item.name}”? Nie można usunąć poświadczenia przypisanego do projektu.`))return
    setBusy(true); setError(''); setMessage('')
    try{
      await request<{status:string}>(`/credentials/${item.id}`,{method:'DELETE'})
      setMessage('Poświadczenie usunięte.')
      await load()
    }catch(c){setError(c instanceof Error?c.message:'Nie udało się usunąć poświadczenia')}
    finally{setBusy(false)}
  }

  return <>
    <div className="page-heading"><div><h1>Poświadczenia</h1><p className="muted">Centralny magazyn tokenów GitHub i kluczy SSH. Sekret jest szyfrowany i nie jest zwracany przez API.</p></div></div>
    {error&&<div className="error-banner">{error}</div>}
    {message&&<div className="success-banner">{message}</div>}

    <form className="panel form-grid credential-create-form" onSubmit={create}>
      <label>Nazwa<input value={name} onChange={e=>setName(e.target.value)} placeholder="GitHub - konto główne" required/></label>
      <label>Typ<select value={kind} onChange={e=>setKind(e.target.value as 'token'|'ssh_key')}><option value="token">GitHub token</option><option value="ssh_key">SSH key</option></select></label>
      <label className="span-2">Sekret<textarea className="secret-field" value={secret} onChange={e=>setSecret(e.target.value)} placeholder={kind==='token'?'github_pat_…':'-----BEGIN OPENSSH PRIVATE KEY-----'} required/></label>
      <div className="form-actions"><button type="submit" disabled={busy}>{busy?'Zapisywanie…':'Dodaj poświadczenie'}</button></div>
    </form>

    <div className="table-scroll">
      <table>
        <thead><tr><th>Nazwa</th><th>Typ</th><th>Sekret</th><th>Aktualizacja</th><th>Akcje</th></tr></thead>
        <tbody>
          {items.map(item=><tr key={item.id}>
            <td><strong>{item.name}</strong></td>
            <td>{item.kind==='token'?'GitHub token':'SSH key'}</td>
            <td><span className="badge badge-ok">zaszyfrowany</span></td>
            <td>{new Date(item.updated_at).toLocaleString()}</td>
            <td className="actions">
              <button type="button" className="secondary" onClick={()=>{setEditing(item);setReplacement('')}} disabled={busy}>Zmień sekret</button>
              <button type="button" className="danger" onClick={()=>void remove(item)} disabled={busy}>Usuń</button>
            </td>
          </tr>)}
          {items.length===0&&<tr><td colSpan={5} className="muted">Brak zapisanych poświadczeń.</td></tr>}
        </tbody>
      </table>
    </div>

    {editing&&<form className="panel form-grid" onSubmit={rotate}>
      <div className="span-2"><h2>Zmień sekret: {editing.name}</h2><p className="muted">Obecna wartość nie jest wyświetlana. Podaj nową, aby ją zastąpić.</p></div>
      <label className="span-2">Nowy sekret<textarea className="secret-field" value={replacement} onChange={e=>setReplacement(e.target.value)} required autoFocus/></label>
      <div className="form-actions"><button type="button" className="secondary" onClick={()=>{setEditing(null);setReplacement('')}}>Anuluj</button><button type="submit" disabled={busy}>Zapisz nowy sekret</button></div>
    </form>}
  </>
}
