import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo, useId } from 'react'

import { Button } from '@/components/ui/button'
import { Tile } from '@/components/ui/tile'
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

interface GaugeTileProps {
  config: GaugeWidgetConfig
  /** Raw SI value from SignalK, or null when the path is absent. */
  value: number | null
  editing: boolean
  onConfigure: () => void
}

export const GaugeTile = memo(function GaugeTile({ config, value, editing, onConfigure }: GaugeTileProps) {
  const unit = unitOption(config.quantity, config.unit)
  const text = formatQuantity(value, config.quantity, config.unit, config.decimals)
  const converted = value === null ? null : convertFromSI(value, config.quantity, config.unit)
  const zone = activeZone(converted, config.zones)

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
      {config.display === 'radial' && (
        <RadialGauge value={converted} zone={zone} config={config} text={text} unitLabel={unit.label} />
      )}
      {config.display === 'bar' && (
        <BarGauge value={converted} zone={zone} config={config} text={text} unitLabel={unit.label} />
      )}
      {config.display === 'lamp' && <LampGauge value={converted} zone={zone} text={text} />}
      {config.display === 'numeric' && <NumericGauge zone={zone} text={text} unitLabel={unit.label} />}
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

function NumericGauge({ text, unitLabel, zone }: { text: string | null; unitLabel: string; zone: GaugeZone['state'] | null }) {
  return (
    <div className="rounded-md border bg-background/60 px-3 py-3">
      <Readout text={text} unitLabel={unitLabel} zone={zone} size="text-4xl" />
    </div>
  )
}

function LampGauge({ value, zone, text }: { value: number | null; zone: GaugeZone['state'] | null; text: string | null }) {
  const lit = value !== null && value !== 0
  const color = zone ? zoneColorFor(zone) : lit ? 'hsl(var(--primary))' : 'hsl(var(--muted-foreground))'

  return (
    <div className="flex items-center gap-3 rounded-md border bg-background/60 px-3 py-3">
      <svg viewBox="0 0 24 24" className="h-8 w-8 shrink-0" aria-hidden="true">
        <circle cx="12" cy="12" r="9" fill={color} opacity={lit ? 1 : 0.25} />
      </svg>
      <span className="font-display text-2xl tabular-nums leading-none text-gauge-primary">
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

function BarGauge({ value, zone, config, text, unitLabel }: {
  value: number | null
  zone: GaugeZone['state'] | null
  config: GaugeWidgetConfig
  text: string | null
  unitLabel: string
}) {
  const { min, max } = rangeFor(config)
  const fraction = clampFraction(value, min, max)

  return (
    <div className="rounded-md border bg-background/60 px-3 py-3">
      <Readout text={text} unitLabel={unitLabel} zone={zone} size="text-3xl" />
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
function RadialGauge({ value, zone, config, text, unitLabel }: {
  value: number | null
  zone: GaugeZone['state'] | null
  config: GaugeWidgetConfig
  text: string | null
  unitLabel: string
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
    <div className="flex flex-col items-center rounded-md border bg-background/60 px-3 py-3">
      <svg viewBox="0 0 100 74" className="w-full max-w-[180px]" aria-hidden="true">
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

      <Readout text={text} unitLabel={unitLabel} zone={zone} size="text-3xl" />
    </div>
  )
}
