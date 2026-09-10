import type { ReactNode } from 'react'

// Renders a single wind barb: a staff pointing toward the direction the wind
// is coming from, with feathers indicating speed (full barb = 10kt, half
// barb = 5kt, pennant = 50kt).
export function WindBarb({ cx, cy, speedKts, directionDeg }: { cx: number; cy: number; speedKts: number; directionDeg: number }) {
  if (speedKts < 0 || directionDeg < 0) return null

  const color = 'hsl(var(--chart-wind) / 0.85)'

  if (speedKts < 3) {
    return <circle data-testid="forecast-wind-barb" cx={cx} cy={cy} r="5" fill="none" stroke={color} strokeWidth="2.4" />
  }

  const angleRad = (directionDeg * Math.PI) / 180
  const dirX = Math.sin(angleRad)
  const dirY = -Math.cos(angleRad)
  const perpX = -dirY
  const perpY = dirX

  // Doubled from the original 12/5/3.5 - a direction glyph read at arm's
  // length in direct sun on the raised plot band, not up close.
  const staffLen = 24
  const barbLen = 10
  const barbSpacing = 7

  let remaining = Math.round(speedKts / 5) * 5
  const pennants = Math.floor(remaining / 50)
  remaining -= pennants * 50
  const fullBarbs = Math.floor(remaining / 10)
  remaining -= fullBarbs * 10
  const hasHalfBarb = remaining >= 5

  const features: ReactNode[] = []
  let pos = staffLen

  for (let i = 0; i < pennants; i++) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const innerPos = pos - barbSpacing
    const innerX = cx + dirX * innerPos
    const innerY = cy + dirY * innerPos
    const outerX = baseX + perpX * barbLen
    const outerY = baseY + perpY * barbLen
    features.push(<polygon key={`pennant-${i}`} points={`${baseX},${baseY} ${innerX},${innerY} ${outerX},${outerY}`} fill={color} />)
    pos -= barbSpacing
  }

  for (let i = 0; i < fullBarbs; i++) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const outerX = baseX + perpX * barbLen
    const outerY = baseY + perpY * barbLen
    features.push(<line key={`full-${i}`} x1={baseX} y1={baseY} x2={outerX} y2={outerY} stroke={color} strokeWidth="2.8" strokeLinecap="round" />)
    pos -= barbSpacing
  }

  if (hasHalfBarb) {
    const baseX = cx + dirX * pos
    const baseY = cy + dirY * pos
    const outerX = baseX + perpX * (barbLen / 2)
    const outerY = baseY + perpY * (barbLen / 2)
    features.push(<line key="half" x1={baseX} y1={baseY} x2={outerX} y2={outerY} stroke={color} strokeWidth="2.8" strokeLinecap="round" />)
  }

  return (
    <g data-testid="forecast-wind-barb">
      <line x1={cx} y1={cy} x2={cx + dirX * staffLen} y2={cy + dirY * staffLen} stroke={color} strokeWidth="2.8" strokeLinecap="round" />
      <circle cx={cx} cy={cy} r="2.8" fill={color} />
      {features}
    </g>
  )
}

export function WaveDirectionArrow({ cx, cy, directionDeg }: { cx: number; cy: number; directionDeg: number }) {
  if (directionDeg < 0) return null

  const color = 'hsl(var(--chart-wave) / 0.85)'
  const angleRad = ((directionDeg + 180) * Math.PI) / 180
  const dirX = Math.sin(angleRad)
  const dirY = -Math.cos(angleRad)
  const perpX = -dirY
  const perpY = dirX

  // Scaled with WindBarb's geometry, not independently. The two glyph
  // families sit in the same 35px band above charts of the same height, one
  // above the wind plot and one above the wave plot, and a reader moving
  // between them reads them as one vocabulary. Doubling the barbs and leaving
  // these behind made the wave row look like the quieter statement, which is
  // not a claim the data supports.
  const len = 18
  const headSize = 8
  const tipX = cx + dirX * len
  const tipY = cy + dirY * len
  const tailX = cx - dirX * len
  const tailY = cy - dirY * len
  const headBaseX = tipX - dirX * headSize
  const headBaseY = tipY - dirY * headSize
  const leftX = headBaseX + perpX * (headSize * 0.6)
  const leftY = headBaseY + perpY * (headSize * 0.6)
  const rightX = headBaseX - perpX * (headSize * 0.6)
  const rightY = headBaseY - perpY * (headSize * 0.6)

  return (
    <g data-testid="forecast-wave-arrow">
      <line x1={tailX} y1={tailY} x2={tipX} y2={tipY} stroke={color} strokeWidth="2.8" strokeLinecap="round" />
      <polygon points={`${tipX},${tipY} ${leftX},${leftY} ${rightX},${rightY}`} fill={color} />
    </g>
  )
}
