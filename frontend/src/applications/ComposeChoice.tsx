export function ComposeChoice({ value, onChange }: { value: string; onChange(value: string): void }) {
  const declined = value === 'managed' || value === 'dockerfile'
  return <div className="acp-notice" role="group" aria-labelledby="compose-choice-title">
    <h2 id="compose-choice-title">Wykryto Docker Compose. Czy chcesz go użyć?</h2>
    <p>Wybierz sposób wdrożenia przed zapisaniem aplikacji.</p>
    <label className="acp-check"><input type="radio" name="compose_choice" checked={value === 'compose'} onChange={() => onChange('compose')} />Tak — użyj Docker Compose</label>
    <label className="acp-check"><input type="radio" name="compose_choice" checked={declined} onChange={() => onChange('managed')} />Nie — wybierz inny sposób wdrożenia</label>
    {declined && <label>Sposób wdrożenia<select value={value} onChange={(event) => onChange(event.target.value)}><option value="managed">Generowany kontener runtime</option><option value="dockerfile">Dockerfile</option></select></label>}
  </div>
}
