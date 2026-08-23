import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo, useId } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
import { useTelemetryHistory, type TelemetryHistoryPoint } from '@/hooks/use-telemetry-history'
import type { GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'
import { formatQuantity, convertFromSI, unitOption } from '@/lib/quantities'

/**
 * Zone colours reuse the alarm severities (ADR 0038). AGENTS.md reserves raw
 * palette colours for alert semantics, which is exactly what a red zone is.
 */
function zoneColorFor(state: GaugeZone['state']): string {
  switch (state) {
    case 'emergency':
      return 'hsl(0 72% 42%)'
    case 'alarm':
      return 'hsl(0 72% 51%)'
    case 'warn':
      return 'hsl(38 92% 50%)'
    case 'alert':
      return 'hsl(43 96% 56%)'
    case 'normal':
      // Green for "operating correctly", completing the colour language the
      // lamp strip already speaks (ADR 0052). It was the border colour, which
      // made an advisory band invisible — advice you cannot see is not advice.
      return 'hsl(142 71% 45%)'
    default:
      return 'hsl(var(--border))'
  }
}

function zoneTextClass(state: GaugeZone['state'] | null): string {
  switch (state) {
    case 'emergency':
      return 'text-red-600'
    case 'alarm':
      return 'text-red-500'
    case 'warn':
      return 'text-amber-500'
    case 'alert':
      return 'text-amber-400'
    default:
      return 'text-gauge-primary'
  }
}

/** The zone a converted reading falls in, or null when it is in no zone. */
function activeZone(value: number | null, zones: GaugeZone[] | undefined): GaugeZone['state'] | null {
  if (value === null || !zones) return null
  const hit = zones.find((zone) => value >= zone.from && value <= zone.to)
  return hit ? hit.state : null
}

/**
 * How much room a gauge has. `full` is a gauge alone in its own tile; `compact`
 * is one member of a gauge group (ADR 0049), where the group tile supplies the
 * single border and N nested ones would just be noise.
 */
export type GaugeDensity = 'full' | 'compact'

const SHELL: Record<GaugeDensity, string> = {
  full: 'rounded-md border bg-background/60 px-3 py-3',
  compact: '',
}

interface GaugeBodyProps {
  config: GaugeWidgetConfig
  /** Raw SI value from SignalK, or null when the path is absent. */
  value: number | null
  density?: GaugeDensity
}

/**
 * One gauge's readout, without a tile around it.
 *
 * Split out of GaugeTile so a gauge group can render N of these inside one
 * Tile without forking the four display kinds. AGENTS.md's "no new primitives"
 * rule governs bespoke domain tiles; ADR 0039 carved it open for generic
 * user-configurable renderers, and ADR 0049 widens that carve-out to cover a
 * renderer shared between the standalone and grouped cases.
 */
export function GaugeBody({ config, value, density = 'full' }: GaugeBodyProps) {
  const unit = unitOption(config.quantity, config.unit)
  const text = formatQuantity(value, config.quantity, config.unit, config.decimals)
  const converted = value === null ? null : convertFromSI(value, config.quantity, config.unit)
  const zone = activeZone(converted, config.zones)

  switch (config.display) {
    case 'radial':
      return <RadialGauge value={converted} zone={zone} config={config} text={text} unitLabel={unit.label} density={density} />
    case 'bar':
      return <BarGauge value={converted} zone={zone} config={config} text={text} unitLabel={unit.label} density={density} />
    case 'lamp':
      return <LampGauge value={converted} zone={zone} text={text} density={density} />
    case 'trend':
      return <TrendGauge zone={zone} config={config} text={text} unitLabel={unit.label} density={density} />
    default:
      return <NumericGauge zone={zone} text={text} unitLabel={unit.label} density={density} />
  }
}

interface GaugeTileProps {
  config: GaugeWidgetConfig
  /** Raw SI value from SignalK, or null when the path is absent. */
  value: number | null
  editing: boolean
  onConfigure: () => void
}

export const GaugeTile = memo(function GaugeTile({ config, value, editing, onConfigure }: GaugeTileProps) {
  const title = config.label.trim() || config.path

  return (
    <Tile
      title={title}
      icon={<GaugeIcon className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        editing ? (
          <Button size="sm" variant="ghost" onClick={onConfigure} aria-label={`Configure ${title}`}>
            <Settings2 className="size-3.5" />
          </Button>
        ) : undefined
      }
    >
      <GaugeBody config={config} value={value} />
    </Tile>
  )
})

/**
 * The structural dash, never a zero. A gauge reading 0 when it means "no data"
 * is the dangerous failure — AGENTS.md's zero-state rule exists for this.
 */
function Readout({ text, unitLabel, zone, size }: { text: string | null; unitLabel: string; zone: GaugeZone['state'] | null; size: string }) {
  return (
    <div className="flex items-baseline gap-1 min-w-0">
      <span className={`font-display ${size} tabular-nums leading-none tracking-tight truncate ${zoneTextClass(zone)}`}>
        {text ?? '--'}
      </span>
      {unitLabel && <span className="text-[11px] leading-none text-muted-foreground">{unitLabel}</span>}
    </div>
  )
}

function NumericGauge({ text, unitLabel, zone, density }: { text: string | null; unitLabel: string; zone: GaugeZone['state'] | null; density: GaugeDensity }) {
  return (
    <div className={SHELL[density]}>
      <Readout text={text} unitLabel={unitLabel} zone={zone} size={density === 'compact' ? 'text-2xl' : 'text-4xl'} />
    </div>
  )
}

function LampGauge({ value, zone, text, density }: { value: number | null; zone: GaugeZone['state'] | null; text: string | null; density: GaugeDensity }) {
  const lit = value !== null && value !== 0
  const color = zone ? zoneColorFor(zone) : lit ? 'hsl(var(--primary))' : 'hsl(var(--muted-foreground))'
  const compact = density === 'compact'

  return (
    <div className={`flex items-center gap-3 ${SHELL[density]}`}>
      <svg viewBox="0 0 24 24" className={compact ? 'h-6 w-6 shrink-0' : 'h-8 w-8 shrink-0'} aria-hidden="true">
        <circle cx="12" cy="12" r="9" fill={color} opacity={lit ? 1 : 0.25} />
      </svg>
      <span className={`font-display ${compact ? 'text-xl' : 'text-2xl'} tabular-nums leading-none text-gauge-primary`}>
        {text === null ? '--' : lit ? 'ON' : 'OFF'}
      </span>
    </div>
  )
}

function rangeFor(config: GaugeWidgetConfig): { min: number; max: number } {
  const min = config.min ?? 0
  const max = config.max ?? 100
  return max > min ? { min, max } : { min: 0, max: 100 }
}

function clampFraction(value: number | null, min: number, max: number): number | null {
  if (value === null) return null
  return Math.max(0, Math.min(1, (value - min) / (max - min)))
}

function BarGauge({ value, zone, config, text, unitLabel, density }: {
  value: number | null
  zone: GaugeZone['state'] | null
  config: GaugeWidgetConfig
  text: string | null
  unitLabel: string
  density: GaugeDensity
}) {
  const { min, max } = rangeFor(config)
  const fraction = clampFraction(value, min, max)

  return (
    <div className={SHELL[density]}>
      <Readout text={text} unitLabel={unitLabel} zone={zone} size={density === 'compact' ? 'text-xl' : 'text-3xl'} />
      <svg viewBox="0 0 100 8" preserveAspectRatio="none" className="mt-2 w-full" height="8" aria-hidden="true">
        <rect x="0" y="2" width="100" height="4" rx="2" fill="hsl(var(--muted))" />
        {(config.zones ?? []).map((z, index) => {
          const from = clampFraction(z.from, min, max) ?? 0
          const to = clampFraction(z.to, min, max) ?? 0
          return (
            <rect key={index} x={from * 100} y="2" width={Math.max(0, (to - from) * 100)} height="4"
              fill={zoneColorFor(z.state)} opacity="0.35" />
          )
        })}
        {fraction !== null && (
          <rect x="0" y="2" width={fraction * 100} height="4" rx="2"
            fill={zone ? zoneColorFor(zone) : 'hsl(var(--primary))'} />
        )}
      </svg>
    </div>
  )
}

/**
 * Hand-rolled SVG rather than a charting dependency: depth-sparkline.tsx set
 * that precedent, and ADR 0012 makes a point of the dashboard having no chart
 * library. A 240-degree arc is the marine instrument convention.
 */
function RadialGauge({ value, zone, config, text, unitLabel, density }: {
  value: number | null
  zone: GaugeZone['state'] | null
  config: GaugeWidgetConfig
  text: string | null
  unitLabel: string
  density: GaugeDensity
}) {
  const gradientId = useId()
  const { min, max } = rangeFor(config)
  const fraction = clampFraction(value, min, max)

  const startAngle = 150
  const sweep = 240
  const radius = 38
  const cx = 50
  const cy = 50

  const pointAt = (angleDeg: number) => {
    const radians = (angleDeg * Math.PI) / 180
    return { x: cx + radius * Math.cos(radians), y: cy + radius * Math.sin(radians) }
  }

  const arcPath = (fromFraction: number, toFraction: number) => {
    const from = pointAt(startAngle + sweep * fromFraction)
    const to = pointAt(startAngle + sweep * toFraction)
    const large = sweep * (toFraction - fromFraction) > 180 ? 1 : 0
    return `M ${from.x} ${from.y} A ${radius} ${radius} 0 ${large} 1 ${to.x} ${to.y}`
  }

  return (
    <div className={`flex flex-col items-center ${SHELL[density]}`}>
      <svg viewBox="0 0 100 74" className={density === 'compact' ? 'w-full max-w-[120px]' : 'w-full max-w-[180px]'} aria-hidden="true">
        <defs>
          <linearGradient id={gradientId} x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="hsl(var(--primary))" stopOpacity="0.7" />
            <stop offset="100%" stopColor="hsl(var(--primary))" />
          </linearGradient>
        </defs>

        <path d={arcPath(0, 1)} fill="none" stroke="hsl(var(--muted))" strokeWidth="8" strokeLinecap="round" />

        {(config.zones ?? []).map((z, index) => {
          const from = clampFraction(z.from, min, max) ?? 0
          const to = clampFraction(z.to, min, max) ?? 0
          if (to <= from) return null
          return (
            <path key={index} d={arcPath(from, to)} fill="none" stroke={zoneColorFor(z.state)}
              strokeWidth="8" strokeLinecap="butt" opacity="0.4" />
          )
        })}

        {fraction !== null && fraction > 0 && (
          <path d={arcPath(0, fraction)} fill="none"
            stroke={zone ? zoneColorFor(zone) : `url(#${gradientId})`}
            strokeWidth="8" strokeLinecap="round" />
        )}
      </svg>

      <Readout text={text} unitLabel={unitLabel} zone={zone} size={density === 'compact' ? 'text-xl' : 'text-3xl'} />
    </div>
  )
}

/**
 * A sparkline over an arbitrary path's history (ADR 0051).
 *
 * The live reading stays the hero number and the line is secondary, per
 * AGENTS.md's rule that trend presentation stays behind the real-time readout.
 * Hand-rolled SVG following depth-sparkline.tsx, since the dashboard carries no
 * chart library.
 */
function TrendGauge({ zone, config, text, unitLabel, density }: {
  zone: GaugeZone['state'] | null
  config: GaugeWidgetConfig
  text: string | null
  unitLabel: string
  density: GaugeDensity
}) {
  const window = config.window ?? '3h'
  const { points, error } = useTelemetryHistory(config.path, window, config.path.trim() !== '')

  return (
    <div className={SHELL[density]}>
      <Readout text={text} unitLabel={unitLabel} zone={zone} size={density === 'compact' ? 'text-xl' : 'text-3xl'} />
      {error ? (
        // Never an empty chart: a flat line drawn because no database exists
        // reads exactly like a sensor holding steady.
        <p className="mt-2 text-[10px] uppercase tracking-wider text-muted-foreground">{error}</p>
      ) : (
        <TrendLine points={points} config={config} window={window} />
      )}
    </div>
  )
}

function TrendLine({ points, config, window }: {
  points: TelemetryHistoryPoint[]
  config: GaugeWidgetConfig
  window: string
}) {
  if (points.length < 2) {
    return (
      <p className="mt-2 text-[10px] uppercase tracking-wider text-muted-foreground">
        Collecting history · {window}
      </p>
    )
  }

  const converted = points.map((p) => convertFromSI(p.value, config.quantity, config.unit))
  // The gauge's own scale when it has one, so a trend and a dial of the same
  // path agree; otherwise the data's own range with a little headroom.
  const min = config.min ?? Math.min(...converted)
  const rawMax = config.max ?? Math.max(...converted)
  const max = rawMax > min ? rawMax : min + 1

  const width = 100
  const height = 28
  const x = (index: number) => (index / (converted.length - 1)) * width
  const y = (value: number) => height - Math.max(0, Math.min(1, (value - min) / (max - min))) * height

  const d = converted.map((v, i) => `${i === 0 ? 'M' : 'L'} ${x(i).toFixed(2)} ${y(v).toFixed(2)}`).join(' ')

  return (
    <div className="mt-2">
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="w-full" height={height} aria-hidden="true">
        {(config.zones ?? []).map((z, index) => {
          const top = y(Math.max(z.from, z.to))
          const bottom = y(Math.min(z.from, z.to))
          return (
            <rect key={index} x="0" y={top} width={width} height={Math.max(0, bottom - top)}
              fill={zoneColorFor(z.state)} opacity="0.18" />
          )
        })}
        <path data-testid="gauge-trend-line" d={d} fill="none" stroke="hsl(var(--primary))" strokeWidth="1.5"
          vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
      </svg>
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground">{window}</span>
    </div>
  )
}
