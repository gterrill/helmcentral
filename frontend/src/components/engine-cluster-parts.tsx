import { Thermometer, Zap } from 'lucide-react'
import { useId, useMemo, type CSSProperties, type ReactNode } from 'react'

import { DIAL_TICK_W, SweepStops } from '@/components/ui/cluster-dial'
import { CoolantIcon, ExhaustIcon, GearboxIcon } from '@/components/ui/telltale-icons'
import { useTelemetryHistory } from '@/hooks/use-telemetry-history'
import { CLUSTER_ICONS, iconForSlot } from '@/lib/cluster-icons'
import type { ClusterFuelRail, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import {
  bulletFor, chargeStageLabel, type BulletBand, type BulletMark, type BulletModel, type BulletTone,
} from '@/lib/engine-cluster-bullet'
import {
  FUEL_BAR_W, FUEL_FOOT_SIZE, FUEL_LEGEND_SIZE, FUEL_R_AFT, FUEL_SCALE_SIZE, FUEL_R_FWD, GAP_ARCS, fuelArc, fuelFeet, fuelLegendPoint, fuelScaleLabels,
  fuelSweep, fuelTicks, fuelTitleLeft, fuelTotalX, gapArcLabelAt, gapArcMark, gapArcPath, type GapArcName,
} from '@/lib/engine-cluster-layout'
import { reading } from '@/lib/cluster-readings'
import { isStale } from '@/lib/staleness'
import { convertFromSI, formatQuantity, unitOption } from '@/lib/quantities'
import { zoneColor, zoneIndexAt } from '@/lib/dial-geometry'
import type { ClusterSide } from '@/lib/dashboard-widgets'

type Values = Record<string, number | null>
type Ages = Record<string, number | null> | undefined

/**
 * The pieces of the Engine Cluster tile that are not the dial. All colour is
 * state: amber or red appear only for a reading in that zone, everything else
 * is the skin's neutral tokens.
 */

/** Inline style for a card surface: one outline, one fill, both tokens. */
export function cardStyle(tone: BulletTone = 'neutral'): CSSProperties {
  return {
    background: 'hsl(var(--cluster-surface))',
    boxShadow: `inset 0 0 0 ${tone === 'neutral' ? 1.5 : 2.5}px ${toneStroke(tone)}`,
  }
}

/** The state colour of a tone, or null for neutral: the one mapping every stroke and fill reads. */
function toneAccent(tone: BulletTone): string | null {
  return tone === 'neutral' ? null : zoneColor(tone)
}

/** An outline in a tone: the state colour, or the card's own hairline when nothing is wrong. */
export function toneStroke(tone: BulletTone): string {
  return toneAccent(tone) ?? 'hsl(var(--cluster-stroke))'
}

/** Text colour for a value in a tone. Amber and red are state colours, not decoration. */
export function toneText(tone: BulletTone): string {
  switch (tone) {
    case 'alarm': return 'text-red-600 dark:text-red-400'
    case 'warn': return 'text-amber-600 dark:text-amber-400'
    case 'alert': return 'text-sky-700 dark:text-sky-300'
    default: return 'text-foreground'
  }
}

const LABEL = 'text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground'

/** A bar or arc fill in a tone: the state colour, or neutral foreground. */
function fillOf(tone: BulletTone): string {
  return toneAccent(tone) ?? 'hsl(var(--foreground))'
}

function bandOpacity(active: boolean): string {
  return active ? 'var(--cluster-band-active)' : 'var(--cluster-band-idle)'
}

/**
 * The bullet graph: a dim track, the slot's zone bands behind it (bright only
 * while the reading is in one), a fill from the foot to the reading, a tick at
 * each real threshold with its number beneath, and a marker.
 */
export function Bullet({ model, width, align }: { model: BulletModel; width: number; align: 'start' | 'end' }) {
  return (
    <div data-bullet="" className={align === 'end' ? 'ml-auto' : ''} style={{ width }}>
      <div className="relative h-4">
        <div className="absolute inset-x-0 top-1 h-2 rounded-xs" style={{ background: 'hsl(var(--cluster-track))' }} />
        {model.bands.map((band: BulletBand, i) => (
          <div key={i} data-band={band.state} data-band-active={band.active ? 'true' : undefined}
            className="absolute top-1 h-2"
            style={{
              left: `${band.left}%`, width: `${band.width}%`,
              background: zoneColor(band.state), opacity: bandOpacity(band.active),
            }} />
        ))}
        {model.position !== null && (
          <div data-bullet-fill="" data-tone={model.tone} className="absolute left-0 top-1.5 h-1 rounded-xs"
            style={{ width: `${model.position}%`, background: fillOf(model.tone) }} />
        )}
        {model.marks.map((mark: BulletMark, i) => (
          <div key={i} data-mark={mark.state} className="absolute top-0 -ml-px h-4 w-0.5"
            style={{ left: `${mark.left}%`, background: zoneColor(mark.state) }} />
        ))}
        {model.position !== null && (
          <div data-bullet-marker="" className="absolute -top-0.5 -ml-0.5 h-5 w-1 rounded-xs bg-foreground"
            style={{ left: `${model.position}%`, boxShadow: '0 0 0 1.5px hsl(var(--cluster-surface))' }} />
        )}
      </div>
      <div className="relative -mt-1 h-3.5">
        {model.marks.map((mark: BulletMark, i) => (
          <span key={i} className="absolute top-0 whitespace-nowrap font-display text-xs leading-[14px]"
            style={{
              left: `${mark.left}%`,
              transform: mark.labelSide === 'before' ? 'translateX(-100%) translateX(-4px)' : 'translateX(4px)',
              color: zoneColor(mark.state),
            }}>
            {mark.label}
          </span>
        ))}
      </div>
    </div>
  )
}

/** The chosen icon, or the one inferred from what the card is measuring. */
export function SlotIcon({ name, slot, className }: {
  name: string | undefined
  slot: { path: string; quantity: string }
  className: string
}) {
  const Icon = CLUSTER_ICONS[name ?? ''] ?? CLUSTER_ICONS[iconForSlot(slot)] ?? CLUSTER_ICONS.gauge
  return <Icon className={className} aria-hidden="true" />
}

/**
 * One readout card's content. The outline is drawn separately in the tile's
 * SVG layer; this sits over it, mirrored so the label stays on the dial side
 * and the value on the outer edge.
 */
export function ReadoutContent({ slot, icon, label, values, ages, side, width, bulletWidth, index }: {
  slot: GaugeWidgetConfig
  icon: string | undefined
  label: string
  values: Values
  ages: Ages
  side: ClusterSide
  width: number
  bulletWidth: number
  index: number
}) {
  const model = bulletFor(slot, values, ages)
  const stbd = side === 'stbd'
  return (
    <div data-testid={`cluster-corner-${index}`} data-tone={model.tone}
      className="flex h-24 flex-col gap-1 pt-2" style={{ width }}>
      <div className={`flex h-4 min-w-0 items-center gap-2 ${stbd ? 'flex-row-reverse' : ''}`}>
        <SlotIcon name={icon} slot={slot} className="size-[18px] shrink-0 text-primary" />
        <span className={`${LABEL} truncate`}>{label}</span>
        {model.stale && (
          <span className="shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 text-xs leading-none text-amber-600 dark:text-amber-400">
            Stale
          </span>
        )}
      </div>
      <div className={`flex h-7 items-baseline gap-1.5 ${stbd ? 'justify-start' : 'justify-end'}`}>
        <span data-reading="" className={`font-display text-3xl font-semibold leading-none tabular-nums ${toneText(model.tone)}`}>
          {model.text ?? '--'}
        </span>
        <span className="text-sm text-muted-foreground">{model.unit}</span>
      </div>
      <Bullet model={model} width={bulletWidth} align={stbd ? 'start' : 'end'} />
    </div>
  )
}

/** The icon each gap arc carries. */
const ARC_ICONS: Record<GapArcName, typeof CoolantIcon> = { left: CoolantIcon, right: GearboxIcon }

/**
 * One of the two arcs in the dial's gap. Nothing is drawn for a slot that is
 * not bound: an empty track would read as a sensor reporting nothing.
 */
export function GapArc({ name, slot, values, ages }: {
  name: GapArcName
  slot: GaugeWidgetConfig | undefined
  values: Values
  ages: Ages
}) {
  if (!slot) return null
  const model = bulletFor(slot, values, ages)
  const min = slot.min ?? 0
  const max = slot.max ?? 100
  const span = max - min || 1
  const frac = (v: number) => Math.max(0, Math.min(1, (v - min) / span))
  const f = model.converted === null ? null : frac(model.converted)
  const at = gapArcLabelAt(name)
  const Icon = ARC_ICONS[name]

  return (
    <g data-gap-arc={name} data-tone={model.tone}>
      <path d={gapArcPath(name, 0, 1)} fill="none" strokeWidth={10} style={{ stroke: 'hsl(var(--cluster-track))' }} />
      {(slot.zones ?? []).map((zone, i) => {
        const from = frac(Math.min(zone.from, zone.to))
        const to = frac(Math.max(zone.from, zone.to))
        const active = i === zoneIndexAt(model.converted, slot.zones)
        return <path key={i} data-band={zone.state} data-band-active={active ? 'true' : undefined}
          d={gapArcPath(name, from, to)} fill="none" strokeWidth={10}
          style={{ stroke: zoneColor(zone.state), opacity: bandOpacity(active) }} />
      })}
      {f !== null && (
        <>
          <path d={gapArcPath(name, 0, f)} fill="none" strokeWidth={4} style={{ stroke: fillOf(model.tone) }} />
          <path d={gapArcMark(name, f)} fill="none" strokeWidth={4} strokeLinecap="round" style={{ stroke: 'hsl(var(--foreground))' }} />
        </>
      )}
      <foreignObject x={at.x - 50} y={at.y - 13} width={100} height={26}>
        <div className={`flex h-full items-center justify-center gap-1 whitespace-nowrap ${toneText(model.tone)}`}>
          <Icon className="size-5 shrink-0 text-muted-foreground" />
          <span className="font-display text-xl font-semibold tabular-nums">
            {model.text === null ? '--' : `${model.text}°`}
          </span>
        </div>
      </foreignObject>
    </g>
  )
}

export function gapArcNames(): GapArcName[] {
  return Object.keys(GAP_ARCS) as GapArcName[]
}

/**
 * The fuel fan on the outer edge: up to two tanks as concentric arcs, a scale
 * along the foot of them and the litres total underneath. Fill colour follows
 * the tank's own levels (ADR 0214), the same ones its alarms use, so the amber is
 * not a number baked in here.
 */
export function FuelFan({ config, title, side, values, ages }: {
  config: ClusterFuelRail
  title: string
  side: ClusterSide
  values: Values
  ages: Ages
}) {
  const radii = [FUEL_R_FWD, FUEL_R_AFT]
  const bars = config.bars.slice(0, radii.length).map((bar, i) => {
    const level = bulletFor(bar.level, values, ages)
    const rawLevel = level.stale ? null : (values[bar.level.path] ?? null)
    // A capacity that has gone stale is as unknown as a stale level: the total must not mix an old figure in.
    const rawCap = isStale(ages?.[bar.capacity.path] ?? null) ? null : (values[bar.capacity.path] ?? null)
    const volume = rawLevel === null || rawCap === null ? null : rawLevel * rawCap
    const percent = level.converted
    return {
      r: radii[i], level, percent, volume, rawCap,
      label: bar.level.label.trim() || (i === 0 ? 'FWD' : 'AFT'),
    }
  })
  const known = bars.length > 0 && bars.every((b) => b.volume !== null)
  const volumeSlot = config.bars[0]?.capacity
  const total = known ? bars.reduce((sum, b) => sum + (b.volume ?? 0), 0) : null
  const capacity = known ? bars.reduce((sum, b) => sum + (b.rawCap ?? 0), 0) : null
  const fmt = (v: number | null) => (volumeSlot ? formatQuantity(v, volumeSlot.quantity, volumeSlot.unit, 0) : null)
  const unit = volumeSlot ? unitOption(volumeSlot.quantity, volumeSlot.unit).label : ''
  const ticks = fuelTicks(side)
  const feet = fuelFeet(side)
  const left = fuelTitleLeft(side)
  const gradientId = useId()
  const fuelLabel = title.trim() ? `${title.trim()} fuel` : 'Fuel'

  return (
    <g data-fuel-fan="" data-side={side}>
      <defs>
        {/* The dial's own sweep, fixed to the scale and not to each fill, so a half-full tank shows
            half the climb. One run per bar, to its own head, so two full tanks end equally bright.
            Amber and red are left for a tank level alert. */}
        {bars.map((bar, i) => {
          const sweep = fuelSweep(side, bar.r)
          return (
            <linearGradient key={i} id={`${gradientId}-${i}`} gradientUnits="userSpaceOnUse"
              x1={0} y1={sweep.foot} x2={0} y2={sweep.top}>
              <SweepStops />
            </linearGradient>
          )
        })}
      </defs>
      {bars.map((bar, i) => {
        const f = bar.percent === null ? 0 : Math.max(0, Math.min(1, bar.percent / 100))
        return (
          <g key={i} data-fuel-bar={i} data-bar-state={bar.percent === null ? 'absent' : 'reading'} data-tone={bar.level.tone}>
            <path d={fuelArc(side, bar.r, 1)} fill="none" strokeWidth={FUEL_BAR_W}
              strokeDasharray={bar.percent === null ? '6 8' : undefined}
              style={{ stroke: 'hsl(var(--fuel-track))', opacity: bar.percent === null ? 0.55 : 1 }} />
            {f > 0 && (
              <path d={fuelArc(side, bar.r, f)} fill="none" strokeWidth={FUEL_BAR_W}
                style={{
                  stroke: bar.level.tone === 'neutral' ? `url(#${gradientId}-${i})` : fillOf(bar.level.tone),
                  filter: bar.level.tone === 'neutral' ? 'var(--fuel-bar-glow)' : undefined,
                }} />
            )}
          </g>
        )
      })}
      <path d={ticks.minor} fill="none" strokeWidth={DIAL_TICK_W.minor} style={{ stroke: 'hsl(var(--fuel-tick-minor))' }} />
      <path d={ticks.major} fill="none" strokeWidth={DIAL_TICK_W.major} style={{ stroke: 'hsl(var(--fuel-tick-major))' }} />
      {fuelScaleLabels(side).map((s) => (
        <text key={s.value} x={s.x} y={s.y} fontSize={FUEL_SCALE_SIZE} fontWeight="600" textAnchor="middle" dominantBaseline="central"
          style={{ fill: 'hsl(var(--fuel-label))' }}>{s.value}</text>
      ))}
      {bars.map((bar, i) => (
        <text key={`foot${i}`} x={feet[i].x} y={feet[i].y} fontSize={FUEL_FOOT_SIZE} fontWeight="600" letterSpacing="1"
          textAnchor={feet[i].anchor} dominantBaseline="central" style={{ fill: 'hsl(var(--muted-foreground))' }}>
          {bar.label.toUpperCase()}
        </text>
      ))}
      {bars.map((bar, i) => {
        if (bar.percent === null) return null
        const legend = fuelLegendPoint(side, bar.r, Math.max(0, Math.min(1, bar.percent / 100)))
        return (
          <text key={`leg${i}`} data-fuel-legend={i} x={legend.x} y={legend.y} fontSize={FUEL_LEGEND_SIZE} fontWeight="700"
            textAnchor="middle" dominantBaseline="central"
            transform={`rotate(${legend.rotate.toFixed(1)} ${legend.x.toFixed(1)} ${legend.y.toFixed(1)})`}
            // Inside the bar the backdrop runs from a faint wash at the foot to a bright blue at the head, so no
            // single text colour reads on all of it. A halo in the card's colour does.
            stroke={legend.inside ? 'hsl(var(--cluster-surface))' : undefined} strokeWidth={3.5} strokeLinejoin="round"
            paintOrder="stroke" style={{ fill: 'hsl(var(--foreground))' }}>
            {Math.round(bar.percent)}%
          </text>
        )
      })}

      <foreignObject x={left} y={129} width={220} height={20}>
        <div className={`flex h-full items-center gap-2 ${side === 'stbd' ? 'justify-end' : 'justify-start'}`}>
          <CLUSTER_ICONS.fuel className="size-[18px] shrink-0 text-primary" aria-hidden="true" />
          <span className={`${LABEL} truncate`}>{fuelLabel}</span>
        </div>
      </foreignObject>
      <foreignObject x={fuelTotalX(side) - 110} y={477} width={220} height={32}>
        <div className="flex h-full items-baseline justify-center gap-1.5 whitespace-nowrap">
          <span data-testid="fuel-total" className="font-display text-3xl font-semibold leading-none tabular-nums text-gauge-primary">
            {fmt(total) ?? '--'}
          </span>
          <span className="text-sm font-semibold text-muted-foreground">
            {known ? `/ ${fmt(capacity)} ${unit}` : 'tank not reporting'}
          </span>
        </div>
      </foreignObject>
    </g>
  )
}

// ── footer ───────────────────────────────────────────────────────────────────

function FooterFrame({ icon, label, extra, flex, children, tone = 'neutral', testId }: {
  icon: ReactNode
  label: string
  extra?: ReactNode
  flex: string
  children: ReactNode
  tone?: BulletTone
  testId: string
}) {
  return (
    <div data-testid={testId} data-tone={tone}
      className="flex h-full min-w-0 flex-col gap-1 rounded-[22px] px-3 pt-2.5"
      style={{ ...cardStyle(tone), flex }}>
      <div className="flex h-4 items-center gap-2">
        <span className="shrink-0 text-primary">{icon}</span>
        <span className={`${LABEL} whitespace-nowrap`}>{label}</span>
        {extra && <div className="ml-auto flex items-center gap-1">{extra}</div>}
      </div>
      {children}
    </div>
  )
}

/** The small temperature figure in a footer card's header. */
function HeaderTemp({ slot, values, ages }: { slot: GaugeWidgetConfig | undefined; values: Values; ages: Ages }) {
  if (!slot) return null
  const model = bulletFor(slot, values, ages)
  return (
    <>
      <Thermometer className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span data-header-temp="" className={`font-display text-base font-semibold tabular-nums ${toneText(model.tone)}`}>
        {model.text === null ? '--' : `${model.text}°`}
      </span>
    </>
  )
}

/** Starter battery voltage in a battery silhouette; filled only in a warn or alarm zone. */
export function BatteryCard({ slot, values, ages, boxed = false }: {
  slot: GaugeWidgetConfig | undefined
  values: Values
  ages: Ages
  /** In a card of its own, like the other footer readings, for the strip's footer column. In the footer row it is a bare silhouette. */
  boxed?: boolean
}) {
  const model = slot ? bulletFor(slot, values, ages) : null
  const tone = model?.tone ?? 'neutral'
  const edge = tone === 'neutral' ? 'hsl(var(--muted-foreground))' : toneStroke(tone)
  const filled = tone === 'warn' || tone === 'alarm'
  return (
    <div data-testid="cluster-battery" data-tone={tone}
      className={boxed ? 'flex h-full items-center justify-center rounded-[22px]' : 'flex h-full shrink-0 items-center px-1'}
      style={boxed ? cardStyle(tone) : undefined}>
      <div className="flex w-[136px] flex-col">
        <div className="flex h-2 justify-between px-[18px]">
          <div className="h-2 w-[18px] rounded-t-xs" style={{ background: edge }} />
          <div className="h-2 w-[18px] rounded-t-xs" style={{ background: edge }} />
        </div>
        <div className={`flex h-[60px] items-center justify-center gap-1 rounded-[5px] ${filled ? 'text-background' : ''}`}
          style={{ background: filled ? edge : 'transparent', boxShadow: `inset 0 0 0 2px ${edge}` }}>
          <span className={`font-display text-4xl font-semibold leading-none tabular-nums ${filled ? '' : toneText(tone)}`}>
            {model?.text ?? '--'}
          </span>
          <span className="mb-3.5 self-end text-base font-semibold">V</span>
        </div>
      </div>
    </div>
  )
}

/** A one-hour amps trace with the BMS charge limit as a dashed line. */
function Sparkline({ path, slot, limit, limitText, enabled }: {
  path: string
  slot: GaugeWidgetConfig
  limit: number | null
  limitText: string | null
  enabled: boolean
}) {
  const { points, error } = useTelemetryHistory(path, '1h', enabled)

  const series = useMemo(() => points
    .map((p) => ({ t: Date.parse(p.time), v: convertFromSI(p.value, slot.quantity, slot.unit) }))
    .filter((p) => Number.isFinite(p.t) && Number.isFinite(p.v)), [points, slot.quantity, slot.unit])

  if (!enabled) return <div className="h-[22px]" />
  // Say why there is no line rather than draw a flat one: a flat trace and a
  // sensor reading steady are indistinguishable, and only one of them is true.
  if (error) return <div data-testid="cluster-spark-error" className="h-[22px] truncate text-xs text-muted-foreground">{error}</div>
  if (series.length < 2) return <div className="h-[22px]" />

  const end = series[series.length - 1].t
  const start = end - 3_600_000
  const top = Math.max(slot.max ?? 0, limit ?? 0, ...series.map((s) => s.v), 1)
  const sx = (t: number) => Math.max(0, Math.min(200, ((t - start) / 3_600_000) * 200))
  const sy = (v: number) => 29.5 - Math.max(0, Math.min(1, v / top)) * 28
  const line = series.map((s, i) => `${i ? 'L' : 'M'}${sx(s.t).toFixed(1)} ${sy(s.v).toFixed(1)}`).join('')
  const area = `${line}L${sx(end).toFixed(1)} 29.5L${sx(series[0].t).toFixed(1)} 29.5Z`
  const limitTop = limit === null ? 0 : (sy(limit) * 22) / 30 - 14

  return (
    <div data-testid="cluster-spark" className="relative h-[22px]">
      <svg width="100%" height="22" viewBox="0 0 200 30" preserveAspectRatio="none" className="absolute left-0 top-0 overflow-visible">
        <path d="M0 29.5H200" fill="none" strokeWidth={1} vectorEffect="non-scaling-stroke" style={{ stroke: 'hsl(var(--cluster-stroke))' }} />
        {limit !== null && (
          <path data-testid="cluster-spark-limit" d={`M0 ${sy(limit).toFixed(1)}H200`} fill="none" strokeWidth={1}
            strokeDasharray="3 3" vectorEffect="non-scaling-stroke" style={{ stroke: 'hsl(var(--muted-foreground))' }} />
        )}
        <path d={area} stroke="none" style={{ fill: 'hsl(var(--primary))', fillOpacity: 'var(--cluster-spark-fill)' }} />
        <path d={line} fill="none" strokeWidth={1.6} strokeLinejoin="round" vectorEffect="non-scaling-stroke"
          style={{ stroke: 'hsl(var(--primary))' }} />
      </svg>
      {limit !== null && limitText && (
        <div className="absolute right-0 whitespace-nowrap font-display text-xs leading-3 text-muted-foreground" style={{ top: limitTop }}>
          BMS {limitText}
        </div>
      )}
      <div className="absolute left-0 whitespace-nowrap font-display text-xs leading-3 text-muted-foreground" style={{ top: limit === null ? 8 : limitTop }}>
        1 HR
      </div>
    </div>
  )
}

/** Alternator: amps large, a one-hour trace under it, watts and regulator stage beside, charge limit as a dashed line. */
export function AlternatorCard({ footer, values, ages, historyEnabled }: {
  footer: {
    altAmps?: GaugeWidgetConfig; altWatts?: GaugeWidgetConfig; altTemp?: GaugeWidgetConfig
    altStage?: GaugeWidgetConfig; altLimit?: GaugeWidgetConfig
  }
  values: Values
  ages: Ages
  historyEnabled: boolean
}) {
  const amps = footer.altAmps ? bulletFor(footer.altAmps, values, ages) : null
  const watts = footer.altWatts ? bulletFor(footer.altWatts, values, ages) : null
  const stage = footer.altStage ? reading(footer.altStage, values, ages) : null
  const limit = footer.altLimit ? bulletFor(footer.altLimit, values, ages) : null
  const stageText = stage?.converted == null ? null : chargeStageLabel(stage.converted)
  const tone = amps?.tone ?? 'neutral'

  return (
    <FooterFrame testId="cluster-alternator" tone={tone} flex="1 1 0"
      icon={<Zap className="size-[18px]" aria-hidden="true" />} label="Alternator"
      extra={<HeaderTemp slot={footer.altTemp} values={values} ages={ages} />}>
      <div className="flex h-[30px] items-baseline justify-between gap-1.5">
        <div className="flex items-center gap-2 self-center">
          <span data-testid="cluster-alt-stage" className="rounded-xs px-1.5 font-display text-xs font-semibold leading-[18px] tracking-wider text-foreground"
            style={{ boxShadow: 'inset 0 0 0 1px hsl(var(--cluster-stroke))' }}>
            {stageText ?? '--'}
          </span>
          <span data-testid="cluster-alt-watts" className="whitespace-nowrap font-display text-sm text-muted-foreground">
            {watts?.text == null ? '--' : `${watts.text} ${watts.unit}`}
          </span>
        </div>
        <div className="flex items-baseline gap-1.5">
          <span data-testid="cluster-alt-amps" className={`font-display text-3xl font-semibold leading-none tabular-nums ${toneText(tone)}`}>
            {amps?.text ?? '--'}
          </span>
          <span className="text-sm text-muted-foreground">A</span>
        </div>
      </div>
      {footer.altAmps ? (
        <Sparkline
          path={footer.altAmps.path} slot={footer.altAmps}
          limit={limit?.converted ?? null}
          limitText={limit?.converted == null ? null : `${Math.round(limit.converted)} A`}
          enabled={historyEnabled}
        />
      ) : <div className="h-[22px]" />}
    </FooterFrame>
  )
}

/** A footer card with a header icon, a big value and a bullet (transmission and exhaust). */
export function ReadingCard({ testId, label, icon, slot, flex, values, ages, bulletWidth, side }: {
  testId: string
  label: string
  icon: ReactNode
  slot: GaugeWidgetConfig | undefined
  flex: string
  values: Values
  ages: Ages
  bulletWidth: number
  side: ClusterSide
}) {
  const model = slot ? bulletFor(slot, values, ages) : null
  return (
    <FooterFrame testId={testId} tone={model?.tone ?? 'neutral'} flex={flex} icon={icon} label={label}>
      <div className="flex h-[30px] items-baseline justify-end gap-1.5">
        <span data-reading="" className={`font-display text-3xl font-semibold leading-none tabular-nums ${toneText(model?.tone ?? 'neutral')}`}>
          {model?.text ?? '--'}
        </span>
        <span className="text-sm text-muted-foreground">{model?.unit ?? ''}</span>
      </div>
      {model ? <Bullet model={model} width={bulletWidth} align={side === 'stbd' ? 'start' : 'end'} /> : <div className="h-[30px]" />}
    </FooterFrame>
  )
}

export const FOOTER_ICONS = {
  trans: <GearboxIcon className="size-[18px]" />,
  egt: <ExhaustIcon className="size-[18px]" />,
}

