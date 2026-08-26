import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'

import { CoolantIcon, ExhaustIcon, GearboxIcon } from '@/components/ui/telltale-icons'
import { memo, type CSSProperties } from 'react'

import { Button } from '@/components/ui/button'
import { DialRing } from '@/components/ui/dial-ring'
import { Tile } from '@/components/ui/tile'
import { CLUSTER_ICONS, iconForSlot } from '@/lib/cluster-icons'
import { bezelOverhangFor, computeCornerMasks, useFitScale, type ClusterCanvasConfig } from '@/lib/cluster-canvas'
import type { ClusterCorner, EngineClusterConfig, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { majorStepFor } from '@/components/gauge-tile'
import { convertFromSI, formatQuantity, unitOption } from '@/lib/quantities'
import type { GaugeZone } from '@/lib/dashboard-widgets'

/**
 * An engine cluster (ADR 0054).
 *
 * The layout is wind-tile.tsx's, with the geometry shared via lib/cluster-canvas:
 * a fixed canvas so each corner card's inner edge can be mask-cut along a circle
 * that lines up with the ring, scaled down to whatever column it lands in.
 *
 * DialRing's rim sits at 130 in a 280 viewBox, the same proportion the compass
 * uses, so the default radiusRatio carries over unchanged.
 */
const RING_BOX = 218
const CARD_W = 196
const CARD_H = 92
const GAP = 14

/**
 * The bezel makes the dial a solid disc, so the canvas has to contain the
 * whole circle. It used to stop where the 250-degree sweep ended and let the
 * empty lower part of the ring box hang off the bottom, which cost nothing
 * while nothing was drawn there and put the dial through the tile's edge the
 * moment a bezel was.
 *
 * So the composition now closes on the circle: the disc is exactly as tall as
 * the block of boxes and centred on it, which is also what makes all four the
 * same size instead of the lower pair being stretched to reach the arc.
 */
const OVERHANG = bezelOverhangFor(RING_BOX, GAP)
const DISC = RING_BOX + 2 * OVERHANG
const RING_TOP = OVERHANG
const CARD_BLOCK_H = DISC

const CLUSTER_CFG: ClusterCanvasConfig = {
  width: 520,
  height: CARD_BLOCK_H,
  ringBox: RING_BOX,
  ringTop: RING_TOP,
  topCardW: CARD_W,
  bottomCardW: CARD_W,
  cardH: CARD_H,
  bottomCardTop: CARD_BLOCK_H - CARD_H,
  gap: GAP,
}

const CLUSTER_MASKS = computeCornerMasks(CLUSTER_CFG)

/**
 * The mask cuts a circle out of each card's ring-facing corner, so a reading
 * laid out edge to edge gets its unit sliced off. Content is padded away from
 * that side and aligned toward the outer corner instead.
 */
const CORNER_LAYOUT = [
  { align: 'items-start text-left', pad: 'pr-12' },
  { align: 'items-end text-right', pad: 'pl-12' },
  { align: 'items-start text-left', pad: 'pr-12' },
  { align: 'items-end text-right', pad: 'pl-12' },
] as const

/** One reading, converted and formatted, or the structural dash when absent. */
function reading(slot: GaugeWidgetConfig, values: Record<string, number | null>) {
  const raw = values[slot.path] ?? null
  const converted = raw === null ? null : convertFromSI(raw, slot.quantity, slot.unit)
  return {
    text: formatQuantity(raw, slot.quantity, slot.unit, slot.decimals),
    unit: unitOption(slot.quantity, slot.unit).label,
    converted,
    zone: zoneFor(converted, slot.zones),
  }
}

/**
 * The band a reading falls in, or `outside` when bands exist and it is in none
 * of them. A value that has left its healthy range must not keep reading
 * normal — answering "is anything wrong" is the whole job here.
 */
function zoneFor(value: number | null, zones: GaugeZone[] | undefined): GaugeZone['state'] | 'outside' | null {
  if (value === null || !zones || zones.length === 0) return null
  const hit = zones.find((zone) => value >= Math.min(zone.from, zone.to) && value <= Math.max(zone.from, zone.to))
  return hit ? hit.state : 'outside'
}

function zoneTextClass(state: ReturnType<typeof zoneFor>): string {
  switch (state) {
    case 'emergency':
    case 'alarm':
      return 'text-red-500'
    case 'warn':
    case 'outside':
      return 'text-amber-500'
    case 'alert':
      return 'text-amber-400'
    default:
      return 'text-gauge-primary'
  }
}

function zoneFill(state: GaugeZone['state']): string {
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
      return 'hsl(142 71% 45%)'
  }
}

/**
 * A hairline of the reading's own scale, using the room the mask leaves in the
 * outer half of each card. The profile's advisory bands were invisible before
 * this: they existed in the config and appeared nowhere.
 */
function ZoneBar({ slot, value }: { slot: GaugeWidgetConfig; value: number | null }) {
  const zones = slot.zones ?? []
  if (zones.length === 0 || slot.min === undefined || slot.max === undefined) return null

  const span = slot.max - slot.min
  if (span <= 0) return null
  const at = (v: number) => Math.max(0, Math.min(1, (v - slot.min!) / span)) * 100

  return (
    <svg data-zone-bar="" viewBox="0 0 100 4" preserveAspectRatio="none"
      className="mt-1 h-1 w-full max-w-[86px]" aria-hidden="true">
      <rect x="0" y="1.5" width="100" height="1" fill="hsl(var(--muted))" />
      {zones.map((zone, index) => {
        const from = at(Math.min(zone.from, zone.to))
        const to = at(Math.max(zone.from, zone.to))
        return <rect key={index} x={from} y="1.5" width={Math.max(0, to - from)} height="1" fill={zoneFill(zone.state)} />
      })}
      {value !== null && (
        <rect x={Math.max(0, at(value) - 0.9)} y="0" width="1.8" height="4" fill="hsl(var(--foreground))" />
      )}
    </svg>
  )
}

/** The chosen icon, or the one inferred from what the box is measuring. */
function SlotIcon({ name, slot, className }: {
  name: string | undefined
  slot: { path: string; quantity: string }
  className: string
}) {
  const Icon = CLUSTER_ICONS[name ?? ''] ?? CLUSTER_ICONS[iconForSlot(slot)] ?? CLUSTER_ICONS.gauge
  return <Icon className={className} aria-hidden="true" />
}

function CornerCard({ corner, values, style, index }: {
  corner: ClusterCorner
  values: Record<string, number | null>
  style: CSSProperties
  index: number
}) {
  const { align, pad } = CORNER_LAYOUT[index] ?? CORNER_LAYOUT[0]
  // One reading gets the hero treatment; several share the card and step down.
  const stacked = corner.rows.length > 1

  return (
    <div
      data-testid={`cluster-corner-${index}`}
      // One hairline, no fill: the tile already has a border and the mask
      // paints a stroke along the cut edge, so a filled card made three border
      // treatments in one composition.
      className={`absolute flex flex-col justify-center rounded-2xl border px-3 py-2 ${
        stacked ? 'gap-1.5' : 'gap-1'
      } ${align} ${pad}`}
      style={style}
    >
      {corner.rows.map((row, rowIndex) => {
        const { text, unit, converted, zone } = reading(row, values)
        const label = row.label.trim() || row.path.split('.').slice(-1)[0]
        return (
          <div key={rowIndex} className={`flex min-w-0 max-w-full flex-col gap-0.5 ${align}`}>
            <span className={`flex max-w-full items-center gap-1 text-[10px] uppercase tracking-[0.14em] text-muted-foreground ${
              align.includes('end') ? 'flex-row-reverse' : ''
            }`}>
              {/* One icon per box, on the first row: a stacked card of three
                  temperatures wants one thermometer, not three. */}
              {rowIndex === 0 && (
                <SlotIcon name={corner.icon} slot={row} className="size-3 shrink-0 text-gauge-secondary" />
              )}
              <span className="truncate">{label}</span>
            </span>
            <span className={`font-display leading-none tabular-nums ${zoneTextClass(zone)} ${stacked ? 'text-base' : 'text-2xl'}`}>
              {text ?? '--'}
              {unit && <span className="ml-0.5 text-[10px] text-muted-foreground">{unit}</span>}
            </span>
            {/* The bar exists to use the dead space in a single-reading card.
                A card stacking three has none, and colour alone carries the
                state there. */}
            {!stacked && <ZoneBar slot={row} value={converted} />}
          </div>
        )
      })}
    </div>
  )
}

/**
 * The symbol a telltale gets, from what it is measuring. Not configurable: the
 * three are a fixed set that exists precisely so the row needs no labels, and a
 * chooser would let an operator break the one thing they are for.
 */
function telltaleIcon(path: string) {
  const p = path.toLowerCase()
  if (p.includes('transmission') || p.includes('gearbox')) return { kind: 'gearbox', Icon: GearboxIcon }
  if (p.includes('exhaust')) return { kind: 'exhaust', Icon: ExhaustIcon }
  return { kind: 'coolant', Icon: CoolantIcon }
}

/**
 * A telltale is a warning light, so it uses the warning-light vocabulary rather
 * than the readout one: grey when there is nothing to say, green while the
 * reading sits in its normal band, red once it is past.
 *
 * Amber covers the tiers in between. A warn band is neither normal operating
 * range nor too hot, and painting it green would be the telltale hiding the one
 * thing it exists to show.
 */
function telltaleClass(state: ReturnType<typeof zoneFor>, value: number | null): string {
  if (value === null) return 'text-muted-foreground'
  switch (state) {
    case 'emergency':
    case 'alarm':
      return 'text-red-500'
    // Above the band the profile calls normal, but this engine's warn and alarm
    // thresholds are null in the profile - nobody has filled them in from the
    // manual yet - so every reading over the normal band lands here. Red would
    // claim an overheat the configuration cannot actually know about.
    case 'outside':
    case 'warn':
      return 'text-amber-500'
    case 'alert':
      return 'text-amber-400'
    // A reading with no bands configured still means the engine is turning and
    // nothing has flagged it, which is what green says.
    default:
      return 'text-emerald-500'
  }
}

/**
 * The telltale row, under the hours notch inside the dial. It sits in the wedge
 * the 250-degree sweep leaves at the bottom, so it costs the composition
 * nothing and puts the lights where the eye already is.
 */
function Telltales({ slots, values }: { slots: GaugeWidgetConfig[]; values: Record<string, number | null> }) {
  return (
    <div data-testid="cluster-telltales" className="mt-2 flex items-center justify-center gap-3">
      {slots.map((slot, index) => {
        const { text, converted, zone } = reading(slot, values)
        const { kind, Icon } = telltaleIcon(slot.path)
        return (
          <span key={index} data-telltale={kind}
            className={`flex items-center gap-1 ${telltaleClass(zone, converted)}`}>
            <Icon className="size-4 shrink-0" />
            <span className="font-display text-[11px] leading-none tabular-nums">{text ?? '--'}</span>
          </span>
        )
      })}
    </div>
  )
}

interface EngineClusterTileProps {
  config: EngineClusterConfig
  values: Record<string, number | null>
  editing: boolean
  onConfigure: () => void
}

export const EngineClusterTile = memo(function EngineClusterTile({
  config, values, editing, onConfigure,
}: EngineClusterTileProps) {
  const [ref, scale] = useFitScale(CLUSTER_CFG.width)
  const title = config.title.trim() || 'Engine'

  const ring = reading(config.ring, values)
  const centre = reading(config.centre, values)
  const ringMin = config.ring.min ?? 0
  const ringMax = config.ring.max ?? 100

  const telltales = config.telltales ?? []
  const { width, height, ringBox, ringTop, topCardW, bottomCardW } = CLUSTER_CFG
  const { topCardH, bottomCardH, bottomCardTop } = CLUSTER_MASKS.geometry
  // The telltales live inside the dial now, so the canvas is exactly the block
  // of boxes again and the disc still spans it.
  const canvasH = height
  const cardStyles: CSSProperties[] = [
    { left: 0, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tl },
    { left: width - topCardW, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tr },
    { left: 0, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.bl },
    { left: width - bottomCardW, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.br },
  ]

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
      {/* The scaled canvas is taken out of flow by the transform, so the
          wrapper is given its scaled height explicitly or the tile keeps a
          gap the size of the unscaled design.
          
          items-start, not items-center: a transform scales paint but not
          layout, so the child still occupies the full unscaled height here.
          Centring it in the scaled-down wrapper pushed half the difference
          out of the top of the tile, which is why the dial climbed out of
          its own card at iPad width and below and stayed put above 520px,
          where the scale is 1 and the two heights agree. */}
      <div ref={ref} className="flex w-full items-start justify-center" style={{ height: canvasH * scale }}>
        <div
          data-cluster-canvas=""
          // shrink-0 or the canvas is not the size it says it is. As a flex
          // item it defaults to flex-shrink:1, so in any column narrower than
          // the design its 520px collapsed to the column width while the
          // corner cards kept their 520-space offsets, putting the right-hand
          // pair outside the tile and the whole page into horizontal scroll.
          // useFitScale is what handles narrow columns; the box itself must
          // not also try to.
          className="relative shrink-0"
          style={{ width, height: canvasH, transform: `scale(${scale})`, transformOrigin: 'top center' }}
        >
          <div
            data-cluster-dial=""
            className="absolute"
            style={{
              left: (width - ringBox) / 2, top: ringTop, width: ringBox, height: ringBox,
              // The dial's bezel paints into the gap the corner masks leave.
              '--dial-bezel-overhang': `${CLUSTER_MASKS.geometry.bezelOverhang}px`,
            } as CSSProperties}
          >
            <DialRing
              value={ring.converted}
              min={ringMin}
              max={ringMax}
              majorStep={majorStepFor(ringMin, ringMax)}
              labelDivisor={config.ring.labelDivisor}
              labelEvery={2}
              zones={config.ring.zones}
            >
              {/* The centre belongs to the ring reading alone. */}
              <div data-testid="cluster-centre" className="flex flex-col items-center">
                <span data-testid="cluster-centre-value"
                  className={`font-display text-5xl leading-none tracking-tight tabular-nums ${zoneTextClass(ring.zone)}`}>
                  {ring.text ?? '--'}
                </span>
                {/* A rule between the reading and what it is measured in. It
                    is what separates an instrument face from a number with a
                    caption under it, and it costs one div. */}
                <div className="mt-1.5 h-0.5 w-[68px] bg-foreground/25" />
                {/* The divisor belongs with the unit it scales, not floating
                    at the dial's foot where the notch now sits. */}
                <span className="mt-1.5 text-sm font-semibold uppercase tracking-[0.16em] text-foreground">
                  {config.ring.label.trim() || ring.unit}
                  {config.ring.labelDivisor ? ` x${config.ring.labelDivisor}` : ''}
                </span>

                {/* Stacked rather than positioned against the dial: an
                    absolutely placed badge has to dodge whatever height the
                    readout above it happens to take. */}
                <span data-testid="cluster-notch"
                  className="mt-2.5 flex items-center gap-1.5 rounded-full border bg-card/90 px-2.5 py-1">
                  <SlotIcon name={config.centreIcon} slot={config.centre} className="size-3 shrink-0 text-gauge-secondary" />
                  <span className="font-display text-sm leading-none tabular-nums text-gauge-secondary">
                    {centre.text ?? '--'}
                  </span>
                  {centre.unit && <span className="text-[10px] text-muted-foreground">{centre.unit}</span>}
                </span>

                {telltales.length > 0 && <Telltales slots={telltales} values={values} />}
              </div>
            </DialRing>
          </div>

          {config.corners.slice(0, CORNER_LAYOUT.length).map((corner, index) => (
            <CornerCard key={index} index={index} corner={corner} values={values} style={cardStyles[index]} />
          ))}
        </div>
      </div>
    </Tile>
  )
})
