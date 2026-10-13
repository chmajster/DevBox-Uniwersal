export function ComposeChoice({ value, onChange }: { value: string; onChange(value: string): void }) {
  return <div className="acp-card acp-mode-choice" role="group" aria-labelledby="compose-choice-title">
    <h2 id="compose-choice-title">Sposób uruchomienia</h2>
    {value !== 'image' && <>
    <label className="acp-check"><input type="radio" aria-label="Docker Compose" name="deployment_mode" value="compose" checked={value === 'compose'} onChange={() => onChange('compose')} /><span><strong>Docker Compose</strong><small>Uruchom usługi z pliku projektu.</small></span></label>
    <label className="acp-check"><input type="radio" aria-label="Automatyczny Docker" name="deployment_mode" value="auto" checked={value === 'auto'} onChange={() => onChange('auto')} /><span><strong>Automatyczny Docker</strong><small>DevBox przygotuje wybrany runtime.</small></span></label>
    <label className="acp-check"><input type="radio" aria-label="Dockerfile aplikacji" name="deployment_mode" value="dockerfile" checked={value === 'dockerfile'} onChange={() => onChange('dockerfile')} /><span><strong>Dockerfile aplikacji</strong><small>Zbuduj własny obraz ze źródeł.</small></span></label>
    </>}
    {value === 'image' && <label className="acp-check"><input type="radio" name="deployment_mode" value="image" checked onChange={() => onChange('image')} />Obraz OCI</label>}
  </div>
}
