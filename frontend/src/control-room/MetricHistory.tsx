import { useState } from 'react'
import { chartSegments, time, type MetricName, type Sample } from './model'

export function MetricHistory({ history, error }: { history: Sample[]; error: string }) {
  const [metric, setMetric] = useState<MetricName>('cpu')
  const [minutes, setMinutes] = useState(120)
  const last = history[history.length - 1]
  const end = last?.at ?? Date.now()
  const requestedStart = end - minutes * 60000
  const selected = history.filter((point) => point.at >= requestedStart)
  // Show the available portion of the requested window, never invented historical points.
  const start = Math.min(end - 10000, selected[0]?.at ?? requestedStart)
  const segments = chartSegments(selected, metric)
  const labels = { cpu: 'CPU', memory: 'Pamięć', disk: 'Dysk' }
  const x = (at: number) => 45 + (at - start) / (end - start) * 450
  const y = (value: number) => 155 - value * 1.3
  const currentValue = !error ? last?.[metric] : null
  return <div className={`metric-history history-${metric}`}>
    <div className="history-controls"><div className="history-tabs" role="group" aria-label="Metryka wykresu">
      {(Object.keys(labels) as MetricName[]).map((key) => <button key={key} aria-pressed={metric === key} onClick={() => setMetric(key)}>{labels[key]}</button>)}
    </div><label><span className="sr-only">Zakres wykresu</span><select value={minutes} onChange={(event) => setMinutes(Number(event.target.value))}><option value={10}>Ostatnie 10 minut</option><option value={30}>Ostatnie 30 minut</option><option value={120}>Ostatnie 2 godziny</option></select></label></div>
    <div className="chart-legend"><span className="status-dot" />{labels[metric]} — {currentValue !== null && currentValue !== undefined ? `${currentValue.toFixed(1)}%` : 'brak bieżącego odczytu'}</div>
    <svg className="history-chart" viewBox="0 0 510 188" role="img" aria-label={`${labels[metric]}: rzeczywiste próbki z bieżącej sesji, zakres ${minutes} minut`}>
      {[0, 25, 50, 75, 100].map((value) => <g key={value}><line className="chart-grid" x1="45" x2="495" y1={y(value)} y2={y(value)} /><text x="35" y={y(value) + 4} textAnchor="end">{value}%</text></g>)}
      {[0, 1, 2, 3, 4].map((tick) => {
        const at = start + tick / 4 * (end - start)
        return <g key={tick}><line className="chart-grid" x1={x(at)} x2={x(at)} y1="25" y2="155" /><text x={x(at)} y="179" textAnchor={tick === 4 ? 'end' : tick === 0 ? 'start' : 'middle'}>{end - start < 300000 ? time(at) : time(at).slice(0, 5)}</text></g>
      })}
      {segments.map((segment, index) => segment.length === 1 ? <circle className="chart-point" key={index} cx={x(segment[0].at)} cy={y(segment[0].value)} r="3" /> : <polyline key={index} className="chart-line" points={segment.map((point) => `${x(point.at)},${y(point.value)}`).join(' ')} />)}
      {!segments.length && <text x="265" y="91" textAnchor="middle">Oczekiwanie na pomiary</text>}
    </svg>
    <p className="history-note">{error ? 'Brak połączenia — wcześniejsze odczyty.' : selected.length < 2 ? 'Zbieranie historii — kolejny pomiar za około 10 s.' : `${selected.length} próbek w tym zakresie.`} Oś czasu obejmuje dostępne próbki od otwarcia panelu.</p>
  </div>
}
