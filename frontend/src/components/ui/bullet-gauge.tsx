export interface BulletGaugeMarker {
  value: number
  label: string
}

export interface BulletGaugeProps {
  value: number | null
  min: number
  max: number
  bandLow: number | null
  bandHigh: number | null
  /** Rendered at most 2: the first solid, the second dashed. */
  markers?: BulletGaugeMarker[]
  unit: string
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v))
}

function fractionOf(v: number, min: number, max: number): number {
  if (max === min) return 0
  return clamp((v - min) / (max - min), 0, 1)
}

/**
 * A hand-rolled SVG bullet gauge for the wall-display tiles - same
 * `viewBox="0 0 100 8"` box as gauge-tile.tsx's BarGauge (ADR 0012: no chart
 * library for a dashboard gauge, ever). Unlike BarGauge this one also shows
 * a forecast range ("band") and up to two reference markers, since the
 * wall-display tiles compare a live reading against a known range rather
 * than just showing a zone-colored value.
 */
export function BulletGauge({ value, min, max, bandLow, bandHigh, markers, unit }: BulletGaugeProps) {
  const valueFraction = value !== null ? fractionOf(value, min, max) : null
  const shownMarkers = (markers ?? []).slice(0, 2)

  let bandX: number | null = null
  let bandWidth: number | null = null
  if (bandLow !== null && bandHigh !== null) {
    const lowFraction = fractionOf(bandLow, min, max)
    const highFraction = fractionOf(bandHigh, min, max)
    bandX = Math.min(lowFraction, highFraction) * 100
    bandWidth = Math.abs(highFraction - lowFraction) * 100
  }

  return (
    <div>
      <svg viewBox="0 0 100 8" preserveAspectRatio="none" className="w-full" height="8" aria-hidden="true">
        <rect data-testid="bullet-gauge-track" x="0" y="2" width="100" height="4" rx="2" fill="hsl(var(--muted))" />

        {bandX !== null && bandWidth !== null && (
          <rect
            data-testid="bullet-gauge-band"
            x={bandX}
            y="2"
            width={bandWidth}
            height="4"
            fill="hsl(var(--muted-foreground) / 0.25)"
          />
        )}

        {valueFraction !== null && (
          <rect
            data-testid="bullet-gauge-value"
            x="0"
            y="2"
            width={valueFraction * 100}
            height="4"
            rx="2"
            fill="hsl(var(--gauge-primary))"
          />
        )}

        {shownMarkers.map((marker, index) => {
          const x = fractionOf(marker.value, min, max) * 100
          const dashed = index === 1
          return (
            <g key={index} role="img" aria-label={`${marker.label}: ${marker.value} ${unit}`}>
              <line
                x1={x}
                x2={x}
                y1="-1"
                y2="9"
                stroke="hsl(var(--border))"
                strokeWidth="0.75"
                strokeDasharray={dashed ? '1.5 1' : undefined}
              />
            </g>
          )
        })}
      </svg>
      <div className="mt-1 flex justify-between text-[10px] text-muted-foreground">
        <span>{min}{unit}</span>
        <span>{max}{unit}</span>
      </div>
    </div>
  )
}
