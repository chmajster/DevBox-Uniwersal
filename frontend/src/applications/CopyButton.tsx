import { useState } from 'react'
import { Icon } from '../components/Icon'

export function CopyButton({ value, label = 'Kopiuj adres' }: { value: string; label?: string }) {
  const [notice, setNotice] = useState('')
  async function copy() {
    try { await navigator.clipboard.writeText(value); setNotice('Skopiowano') }
    catch { setNotice('Nie udało się skopiować. Zaznacz i skopiuj tekst.') }
  }
  return <span className="app-copy">
    <button type="button" className="secondary-button" onClick={() => void copy()}><Icon name="copy" size={15} />{label}</button>
    {notice && <span role="status">{notice}</span>}
  </span>
}
