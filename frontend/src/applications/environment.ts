export interface EnvironmentRow { name: string; value: string; secret: boolean }
export function parseEnv(text: string): EnvironmentRow[] {
  return text.split(/\r?\n/).flatMap((line, index) => {
    const raw = line.trim()
    if (!raw || raw.startsWith('#')) return []
    const match = raw.match(/^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/)
    if (!match) throw new Error(`Niepoprawny wpis .env w linii ${index + 1}.`)
    let value = match[2]
    if (value.startsWith('"') || value.startsWith("'")) {
      const quote = value[0]
      if (value.length < 2 || !value.endsWith(quote)) throw new Error(`Niezamknięty cudzysłów w linii ${index + 1}.`)
      value = value.slice(1, -1)
    } else value = value.replace(/\s+#.*$/, '')
    return [{ name: match[1], value, secret: /password|passwd|secret|token|private.?key|api.?key|credential/i.test(match[1]) }]
  })
}
export function encodeEnvironment(rows: EnvironmentRow[]) {
  const publicValues: Record<string, string> = {}
  const secrets: Record<string, string> = {}
  for (const row of rows) {
    if (!row.name && !row.value) continue
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(row.name)) throw new Error('Nazwa zmiennej musi składać się z liter, cyfr i podkreśleń.')
    if (row.name in publicValues || row.name in secrets) throw new Error(`Zmienna ${row.name} została wpisana dwukrotnie.`)
    ;(row.secret ? secrets : publicValues)[row.name] = row.value
  }
  return { publicValues, secrets }
}
