import { useCallback, useEffect, useState } from 'react'
import { request } from '../api/client'
import type { UpdateStatus } from '../api/types'
import { Icon } from '../components/Icon'

function shortVersion(value?: string) {
  if (!value) return '—'
  return /^[a-f0-9]{40}$/i.test(value) ? value.slice(0, 10) : value
}

export function UpdatesPage() {
  const [status,setStatus]=useState<UpdateStatus|null>(null)
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  const [message,setMessage]=useState('')

  const load=useCallback(async()=>{
    setError('')
    const data=await request<UpdateStatus>('/update/status')
    setStatus(data)
  },[])

  useEffect(()=>{load().catch(c=>setError(c instanceof Error?c.message:'Nie udało się sprawdzić aktualizacji'))},[load])

  async function apply(){
    if(!window.confirm('Uruchomić aktualizację DevBox z repozytorium Git? Po wdrożeniu usługa zostanie automatycznie zrestartowana.'))return
    setBusy(true);setError('');setMessage('')
    try{
      const result=await request<{status:string;message:string}>('/update/apply',{method:'POST'})
      setMessage(result.message)
      window.setTimeout(()=>load().catch(()=>undefined),4000)
    }catch(c){setError(c instanceof Error?c.message:'Nie udało się uruchomić aktualizacji')}
    finally{setBusy(false)}
  }

  return <>
    <div className="page-heading">
      <div><h1>Aktualizacje</h1><p className="muted">Aktualizacja DevBox Universal bezpośrednio z repozytorium Git.</p></div>
      <button type="button" className="secondary" onClick={()=>load().catch(c=>setError(c instanceof Error?c.message:'Nie udało się sprawdzić aktualizacji'))} disabled={busy}>
        <Icon name="refresh" size={16}/> Sprawdź aktualizacje
      </button>
    </div>
    {error&&<div className="error-banner">{error}</div>}
    {message&&<div className="success-banner">{message}</div>}

    <section className="panel">
      <div className="summary-grid">
        <div><span>Repozytorium</span><strong>{status?.repository??'—'}</strong></div>
        <div><span>Gałąź</span><strong>{status?.ref??'—'}</strong></div>
        <div><span>Wersja zainstalowana</span><strong className="mono">{shortVersion(status?.current_version)}</strong></div>
        <div><span>Najnowszy commit</span><strong className="mono">{shortVersion(status?.latest_version)}</strong></div>
        <div><span>Auto-update</span><strong>{status?.auto_update?'Włączony':'Wyłączony / niedostępny'}</strong></div>
        <div><span>Harmonogram</span><strong>{status?.schedule??'—'}</strong></div>
      </div>
      {status?.last_error&&<div className="warning-banner">Nie udało się pobrać najnowszego commita: {status.last_error}</div>}
      <div className="form-actions" style={{marginTop:16}}>
        <span className="status-chip" data-ok={status?.update_available?'false':'true'}>
          {status?.update_available?'Dostępna aktualizacja':'System aktualny'}
        </span>
        <button type="button" onClick={apply} disabled={busy||!status?.update_available}>
          {busy?'Uruchamianie…':'Aktualizuj teraz'}
        </button>
      </div>
      <p className="muted small">Automatyczny updater sprawdza wskazaną gałąź co 12 godzin. Aktualizacja buduje nowy backend i frontend, wdraża migracje oraz restartuje devbox.service dopiero po instalacji artefaktów.</p>
    </section>
  </>
}
