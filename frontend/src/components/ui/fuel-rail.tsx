import { zoneColor } from '@/components/ui/dial-ring'
import { reading, zoneTextClass } from '@/lib/cluster-readings'
import type { ClusterFuelRail, FuelBarConfig } from '@/lib/dashboard-widgets'
import { formatQuantity, unitOption } from '@/lib/quantities'

/**
 * A curved bar fuel gauge (ADR 0061).
 *
 * The reference MFD's fan is a dial with a very large radius and a narrow
 * sweep: the 0..100 scale runs along the angular axis, ticks are radial
 * segments and each tank is a thick arc at its own constant radius. So the
 * maths is dial-ring.tsx's, at a different aspect ratio, and the same idioms
 * carry over - a polar helper, named radii, theme-token paint, data hooks.
 *
 * Two units to the design pixel, for dial-ring's reason: whole-unit tick
 * placement at this size is far too coarse, and it puts fontSize 20 at the
 * micro-typography scale's 10px once rendered.
 */

const V_W = 224
const V_H = 304
/** Design px. The rendered chart is half the viewBox. */
export const RAIL_W = V_W / 2
const CHART_H = V_H / 2

/**
 * The scale runs straight down the panel: bars on the left, ticks and numbers
 * to their right, on both edges of the tile alike. It was drawn on a very large
 * radius at first, to follow the reference MFD's bowed fan, and mirrored so a
 * facing pair of clusters put their scales inboard. Both are gone: a bowed bar
 * is harder to read against a straight tick than a straight one, and a rail
 * that changes hand between the two engines makes an operator re-learn it at
 * the second tile.
 *
 * `side` therefore only says which edge of the tile the rail attaches to, which
 * is the cluster's business rather than this component's.
 */
const Y_TOP = 20 // 100 percent
const Y_BOTTOM = 284 // empty

/** Left edge of each bar, and the gap that keeps two of them apart. */
const BAR_X = [24, 76]
const BAR_W = 40
const BAR_GAP = 12
/** A zone band rides just off its own bar's right edge, clear of the next one. */
const ZONE_GAP = 7

const TICK_RIGHT = 160
const TICK_MAJOR_LEFT = 126
const TICK_MINOR_LEFT = 140

/** Scale numbers are right-aligned, so 100 and 0 share an edge. */
const LABEL_X = 196
/** The `%` sits outboard of the numbers at mid-scale, as the reference has it. */
const UNIT_X = 202

const MAJOR_STEP = 25
const MINOR_STEP = 5

/** Where a percentage lands on the scale. */
function yFor(percent: number): number {
  return Y_BOTTOM - (clampPercent(percent) / 100) * (Y_BOTTOM - Y_TOP)
}

function clampPercent(v: number): number {
  return Math.max(0, Math.min(100, v))
}

export interface FuelRailProps {
  config: ClusterFuelRail
  values: Record<string, number | null>
  /** Design-unit height, so the rail matches the cluster canvas exactly. */
  height: number
}

/** What one tank contributes: a bar height, a percentage and a volume. */
function barReading(bar: FuelBarConfig, values: Record<string, number | null>) {
  const level = reading(bar.level, values)
  const rawLevel = values[bar.level.path] ?? null
  const rawCapacity = values[bar.capacity.path] ?? null
  // Litres are the product of two readings, so both have to be present. One
  // alone gives a number that looks right and is not.
  const volume = rawLevel === null || rawCapacity === null ? null : rawLevel * rawCapacity
  return {
    label: bar.level.label.trim() || bar.level.path.split('.').slice(-2)[0],
    percent: level.converted,
    percentText: level.text,
    zone: level.zone,
    volume,
    volumeText: formatQuantity(volume, bar.capacity.quantity, bar.capacity.unit, bar.capacity.decimals ?? 0),
  }
}

export function FuelRail({ config, values, height }: FuelRailProps) {
  const bars = config.bars.map((bar) => barReading(bar, values))

  /** A bar, from the foot of the scale up to its own reading. */
  const barLine = (percent: number, index: number) => {
    // Past the two the layout is drawn for, keep stepping by the same pitch
    // rather than stacking every extra bar on the last one's column.
    const left = BAR_X[index] ?? BAR_X[BAR_X.length - 1] + (index - BAR_X.length + 1) * (BAR_W + BAR_GAP)
    return { x1: left + BAR_W / 2, x2: left + BAR_W / 2, y1: Y_BOTTOM, y2: yFor(percent) }
  }
  const zoneX = (index: number) =>
    (BAR_X[index] ?? BAR_X[BAR_X.length - 1] + (index - BAR_X.length + 1) * (BAR_W + BAR_GAP)) + BAR_W + ZONE_GAP

  const ticks: number[] = []
  for (let v = 0; v <= 100 + 1e-9; v += MINOR_STEP) ticks.push(v)

  // Every tank, or no total at all. A sum missing one tank is a plausible
  // number that is wrong, which on a fuel gauge is worse than no number.
  const totalVolume = bars.some((b) => b.volume === null)
    ? null
    : bars.reduce((sum, b) => sum + (b.volume ?? 0), 0)
  const volumeSlot = config.bars[0]?.capacity
  const totalText = volumeSlot
    ? formatQuantity(totalVolume, volumeSlot.quantity, volumeSlot.unit, volumeSlot.decimals ?? 0)
    : null
  const volumeUnit = volumeSlot ? unitOption(volumeSlot.quantity, volumeSlot.unit).label : ''

  return (
    <div
      data-fuel-rail=""
      data-side={config.side}
      className="flex flex-col items-stretch"
      style={{ width: RAIL_W, height }}
    >
      <svg viewBox={`0 0 ${V_W} ${V_H}`} width={RAIL_W} height={CHART_H} aria-hidden="true">
        {ticks.map((v) => {
          const major = Math.abs(v % MAJOR_STEP) < 1e-9
          const y = yFor(v)
          return (
            <line
              key={v} data-tick={major ? 'major' : 'minor'}
              x1={major ? TICK_MAJOR_LEFT : TICK_MINOR_LEFT} y1={y.toFixed(2)}
              x2={TICK_RIGHT} y2={y.toFixed(2)}
              style={{
                stroke: major ? 'hsl(var(--fuel-tick-major))' : 'hsl(var(--fuel-tick-minor))',
                strokeWidth: major ? 7 : 3,
              }}
            />
          )
        })}

        {[0, MAJOR_STEP, MAJOR_STEP * 2, MAJOR_STEP * 3, 100].map((v) => (
          <text
            key={v} x={LABEL_X} y={yFor(v).toFixed(2)} fontSize="20"
            textAnchor="end" dominantBaseline="central" fontWeight="600"
            style={{ fill: 'hsl(var(--fuel-label))' }}
          >
            {v}
          </text>
        ))}
        <text
          data-fuel-unit="" x={UNIT_X} y={yFor(50).toFixed(2)}
          fontSize="18" textAnchor="start" dominantBaseline="central"
          style={{ fill: 'hsl(var(--fuel-tick-minor))' }}
        >
          %
        </text>

        {bars.map((bar, index) => {
          const state = bar.percent === null ? 'absent' : bar.percent <= 0 ? 'empty' : 'reading'
          const track = barLine(100, index)
          const value = barLine(bar.percent ?? 0, index)
          return (
            <g key={index} data-bar-state={state}>
              {/* The track goes dashed when the path is silent. A tank reading
                  zero and a dead sender otherwise draw the same nothing, and
                  the difference is one you have to act on. */}
              <line
                data-fuel-track={index}
                x1={track.x1} x2={track.x2} y1={track.y1} y2={track.y2}
                strokeWidth={BAR_W}
                strokeDasharray={state === 'absent' ? '6 8' : undefined}
                style={{ stroke: 'hsl(var(--fuel-track))', opacity: state === 'absent' ? 0.55 : 1 }}
              />
              {state === 'reading' && (
                <line
                  data-fuel-bar={index}
                  x1={value.x1} x2={value.x2} y1={value.y1} y2={value.y2}
                  strokeWidth={BAR_W} strokeLinecap="butt"
                  style={{ stroke: 'hsl(var(--fuel-bar))', filter: 'var(--fuel-bar-glow)' }}
                />
              )}
              {/* A band on the tank it belongs to, not across the shared scale:
                  a low-fuel mark on one tank must not read as both. */}
              {(config.bars[index]?.level.zones ?? [])
                .filter((zone) => zone.state !== 'normal')
                .map((zone, zoneIndex) => {
                  const x = zoneX(index)
                  return (
                    <line
                      key={zoneIndex} data-zone={zone.state}
                      x1={x} x2={x}
                      y1={yFor(Math.min(zone.from, zone.to)).toFixed(2)}
                      y2={yFor(Math.max(zone.from, zone.to)).toFixed(2)}
                      stroke={zoneColor(zone.state)} strokeWidth="5"
                    />
                  )
                })}
            </g>
          )
        })}
      </svg>

      {/* Readouts as DOM rather than SVG text: they are a KPI stack like every
          other reading on the tile, and they wrap and truncate like one. */}
      <div className="flex flex-1 flex-col justify-end gap-1 px-1 pb-0.5">
        <div className="flex items-end justify-between gap-1">
          {bars.map((bar, index) => (
            <div key={index} data-testid={`fuel-readout-${index}`}
              className="flex min-w-0 flex-1 flex-col items-center gap-0.5">
              <span className="w-full truncate text-center text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
                {bar.label}
              </span>
              <span className={`font-display text-[13px] leading-none tabular-nums ${zoneTextClass(bar.zone)}`}>
                {bar.percentText ?? '--'}
                <span className="ml-0.5 text-[10px] text-muted-foreground">%</span>
              </span>
              <span className="text-[10px] leading-none tabular-nums text-muted-foreground">
                {bar.volumeText ?? '--'}
              </span>
            </div>
          ))}
        </div>

        <div className="h-px w-full bg-border" />

        <div className="flex flex-col items-center gap-0.5">
          <span className="text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
            {config.totalLabel?.trim() || 'Total'}
          </span>
          <span data-testid="fuel-total" className="font-display text-base leading-none tabular-nums text-gauge-primary">
            {totalText ?? '--'}
            {volumeUnit && <span className="ml-0.5 text-[10px] text-muted-foreground">{volumeUnit}</span>}
          </span>
        </div>
      </div>
    </div>
  )
}
