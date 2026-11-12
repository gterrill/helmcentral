/** The small line or bar sample in a chart's key. */
export function LegendSwatch({
  color,
  strokeWidth = 2,
  dasharray,
  kind = 'line',
}: {
  color: string
  strokeWidth?: number
  dasharray?: string
  kind?: 'line' | 'bar'
}) {
  return (
    <svg width="14" height="8" viewBox="0 0 14 8" className="inline-block align-middle" aria-hidden="true">
      {kind === 'bar' ? (
        <rect x="2" y="1" width="10" height="6" rx="1" fill={color} />
      ) : (
        <line x1="0" y1="4" x2="14" y2="4" stroke={color} strokeWidth={strokeWidth} strokeDasharray={dasharray} strokeLinecap="round" />
      )}
    </svg>
  )
}
