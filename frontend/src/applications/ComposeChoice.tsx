export function ComposeChoice({ value, onChange }: { value: string; onChange(value: string): void }) {
  const declined = value === 'managed'
  return <div className="acp-notice" role="group" aria-labelledby="compose-choice-title">
    <h2 id="compose-choice-title">W katalogu aplikacji wykryto Docker Compose</h2>
    <p>Compose jest opcjonalny. Możesz użyć go do wdrożenia wielu usług albo pozostawić DevBox budowanie pojedynczego kontenera runtime.</p>
    <label className="acp-check"><input type="radio" name="compose_choice" checked={value === 'compose'} onChange={() => onChange('compose')} />Użyj Docker Compose</label>
    <label className="acp-check"><input type="radio" name="compose_choice" checked={declined} onChange={() => onChange('managed')} />Nie używaj Compose — uruchom kontener runtime DevBox</label>
  </div>
}
