import { percent } from './model'

export function ResourceGauge({ label, value, detail, tone }: { label: string; value?: number; detail: string; tone: string }) {
  const normalized = percent(value)
  return <div className={`resource-gauge gauge-${tone}`}>
    <div className="gauge-dial" role="img" aria-label={`${label}: ${normalized === null ? 'brak danych' : `${normalized.toFixed(1)}%`}. ${detail}`}>
      <svg viewBox="0 0 120 120" aria-hidden="true"><circle className="gauge-track" cx="60" cy="60" r="51" />
        {normalized !== null && <circle className="gauge-value" cx="60" cy="60" r="51" pathLength="100" strokeDasharray={`${normalized} 100`} transform="rotate(-90 60 60)" />}</svg>
      <div className="gauge-label" aria-hidden="true"><span>{label}</span><strong>{normalized === null ? '—' : `${Math.round(normalized)}%`}</strong></div>
    </div><small>{detail}</small>
  </div>
}
