export function ComposeChoice({ value, onChange }: { value: string; onChange(value: string): void }) {
  return <div className="acp-card" role="group" aria-labelledby="compose-choice-title">
    <h2 id="compose-choice-title">Sposób uruchomienia</h2>
    {value !== 'image' && <>
    <label className="acp-check"><input type="radio" name="deployment_mode" value="compose" checked={value === 'compose'} onChange={() => onChange('compose')} />Existing Docker Compose</label>
    <label className="acp-check"><input type="radio" name="deployment_mode" value="auto" checked={value === 'auto'} onChange={() => onChange('auto')} />DevBox Managed Runtime</label>
    <label className="acp-check"><input type="radio" name="deployment_mode" value="dockerfile" checked={value === 'dockerfile'} onChange={() => onChange('dockerfile')} />Dockerfile aplikacji</label>
    </>}
    {value === 'image' && <label className="acp-check"><input type="radio" name="deployment_mode" value="image" checked onChange={() => onChange('image')} />Obraz OCI</label>}
  </div>
}
