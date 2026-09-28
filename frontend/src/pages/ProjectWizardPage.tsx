import { useRef, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { request } from '../api/client'
import { DirectoryPicker } from '../components/DirectoryPicker'
import type { DeploymentMode, Project, ProjectSourceType } from '../api/types'

interface FormState { name:string; description:string; source_type:ProjectSourceType; repository_url:string; branch:string; local_path:string; runtime:string; deployment_mode:DeploymentMode; working_directory:string; build_command:string; start_command:string; healthcheck:string; auto_start:boolean; credential_kind:''|'token'|'ssh_key'; credential_value:string }
const initial:FormState={name:'',description:'',source_type:'git',repository_url:'',branch:'main',local_path:'',runtime:'',deployment_mode:'native',working_directory:'',build_command:'',start_command:'',healthcheck:'',auto_start:false,credential_kind:'',credential_value:''}

function repositoryNameFromURL(value:string){
 const raw=value.trim()
 if(!raw)return ''
 try{
  const normalized=/^[a-z][a-z0-9+.-]*:\/\//i.test(raw)?raw:`https://${raw}`
  const url=new URL(normalized)
  const segments=url.pathname.split('/').filter(Boolean)
  if(segments.length<2)return ''
  return segments[1].replace(/\.git$/i,'')
 }catch{
  const match=raw.match(/(?:github\.com[:/])[^/]+\/([^/?#]+?)(?:\.git)?(?:[/?#]|$)/i)
  return match?.[1]??''
 }
}
export function ProjectWizardPage(){
 const navigate=useNavigate(); const [step,setStep]=useState(1); const [form,setForm]=useState<FormState>(initial); const [busy,setBusy]=useState(false); const [error,setError]=useState(''); const [directoryBrowserOpen,setDirectoryBrowserOpen]=useState(false); const autoRepositoryName=useRef('');
 const set=<K extends keyof FormState,>(key:K,value:FormState[K])=>setForm(current=>({...current,[key]:value}))
 function setRepositoryURL(value:string){
  const detectedName=repositoryNameFromURL(value)
  setForm(current=>{
   const canAutofill=!current.name.trim()||current.name===autoRepositoryName.current
   if(canAutofill)autoRepositoryName.current=detectedName
   return {...current,repository_url:value,name:canAutofill&&detectedName?detectedName:current.name}
  })
 }
 function setName(value:string){
  autoRepositoryName.current=''
  set('name',value)
 }
 async function submit(event:FormEvent){event.preventDefault();if(step<3){setStep(step+1);return}setBusy(true);setError('');try{const path=form.source_type==='local'?'/projects/import':'/projects';const project=await request<Project>(path,{method:'POST',body:JSON.stringify(form)});navigate(`/apps/${project.id}`)}catch(cause){setError(cause instanceof Error?cause.message:'Failed to create application')}finally{setBusy(false)}}
 return <form className="wizard" onSubmit={submit}><div className="page-heading"><div><h1>Dodaj aplikację</h1><p className="muted">Krok {step} z 3</p></div></div>{error&&<div className="error-banner">{error}</div>}
 {step===1&&<div className="panel form-grid"><label>Nazwa<input value={form.name} onChange={e=>setName(e.target.value)} required/></label><label>Typ źródła<select value={form.source_type} onChange={e=>{const source=e.target.value as ProjectSourceType;set('source_type',source);if(source!=='local')setDirectoryBrowserOpen(false)}}><option value="git">Git repository</option><option value="local">Local directory</option><option value="empty">Empty project</option></select></label><label className="span-2">Opis<textarea value={form.description} onChange={e=>set('description',e.target.value)}/></label>{form.source_type==='git'&&<><label className="span-2">Repository URL<input value={form.repository_url} onChange={e=>setRepositoryURL(e.target.value)} required/></label><label>Branch<input value={form.branch} onChange={e=>set('branch',e.target.value)}/></label><label>Poświadczenie<select value={form.credential_kind} onChange={e=>set('credential_kind',e.target.value as FormState['credential_kind'])}><option value="">Brak</option><option value="token">GitHub token</option><option value="ssh_key">SSH key</option></select></label>{form.credential_kind&&<label className="span-2">Sekret<textarea className="secret-field" value={form.credential_value} onChange={e=>set('credential_value',e.target.value)} required/></label>}</>}{form.source_type==='local'&&<>
  <div className="span-2 path-picker-field">
    <label htmlFor="local-path">Pełna ścieżka katalogu</label>
    <div className="path-picker-row">
      <input id="local-path" value={form.local_path} onChange={e=>set('local_path',e.target.value)} required/>
      <button type="button" className="secondary" onClick={()=>setDirectoryBrowserOpen(open=>!open)}>{directoryBrowserOpen?'Ukryj drzewko':'Przeglądaj…'}</button>
    </div>
  </div>
  {directoryBrowserOpen&&<div className="span-2"><DirectoryPicker value={form.local_path} onSelect={path=>set('local_path',path)} onClose={()=>setDirectoryBrowserOpen(false)}/></div>}
</>}</div>}
 {step===2&&<div className="panel form-grid"><label>Runtime<input list="runtime-options" value={form.runtime} onChange={e=>set('runtime',e.target.value)} placeholder="Wybierz lub wpisz runtime"/><datalist id="runtime-options"><option value="node"/><option value="python"/><option value="php"/><option value="go"/><option value="static"/></datalist></label><label>Deployment mode<select value={form.deployment_mode} onChange={e=>set('deployment_mode',e.target.value as DeploymentMode)}><option value="native">native</option><option value="docker">docker</option></select></label><label className="span-2">Working directory<input value={form.working_directory} onChange={e=>set('working_directory',e.target.value)}/></label><label className="span-2">Build command<input value={form.build_command} onChange={e=>set('build_command',e.target.value)}/></label><label className="span-2">Start command<input value={form.start_command} onChange={e=>set('start_command',e.target.value)}/></label><label className="span-2">Healthcheck<input value={form.healthcheck} onChange={e=>set('healthcheck',e.target.value)}/></label><label className="checkbox"><input type="checkbox" checked={form.auto_start} onChange={e=>set('auto_start',e.target.checked)}/> Auto start</label></div>}
 {step===3&&<div className="panel summary-grid"><div><span>Nazwa</span><strong>{form.name}</strong></div><div><span>Źródło</span><strong>{form.source_type}</strong></div><div><span>Runtime</span><strong>{form.runtime||'nie ustawiono'}</strong></div><div><span>Tryb</span><strong>{form.deployment_mode}</strong></div>{form.source_type==='git'&&<div className="span-2"><span>Repo</span><strong>{form.repository_url} · {form.branch||'default'}</strong></div>}{form.source_type==='local'&&<div className="span-2"><span>Katalog</span><strong>{form.local_path}</strong></div>}</div>}
 <div className="wizard-actions">{step>1&&<button type="button" className="secondary" onClick={()=>setStep(step-1)}>Wstecz</button>}<button type="submit" disabled={busy}>{step<3?'Dalej':busy?'Zapisywanie…':'Utwórz aplikację'}</button></div></form>
}
