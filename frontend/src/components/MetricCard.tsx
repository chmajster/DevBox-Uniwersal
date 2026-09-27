export function MetricCard({
  label,
  value,
  detail,
  percent
}: {
  label: string
  value: string
  detail?: string
  percent?: number
}) {
  const bounded = percent === undefined ? undefined : Math.max(0, Math.min(100, percent))
  return (
    <article className="metric-card">
      <span>{label}</span>
      <strong>{value}</strong>
      {bounded !== undefined && <progress max={100} value={bounded}>{Math.round(bounded)}%</progress>}
      {detail && <small>{detail}</small>}
    </article>
  )
}
