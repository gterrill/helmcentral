import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'
import { memo, type CSSProperties } from 'react'

import { Button } from '@/components/ui/button'
import { DialRing } from '@/components/ui/dial-ring'
import { Tile } from '@/components/ui/tile'
import { CLUSTER_ICONS, iconForSlot } from '@/lib/cluster-icons'
import { arcEndFraction } from '@/components/ui/dial-ring'
import { computeCornerMasks, useFitScale, type ClusterCanvasConfig } from '@/lib/cluster-canvas'
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
const RING_TOP = 60
const TOP_CARD_H = 92
const BOTTOM_CARD_H = 118

/**
 * The lower cards end where the sweep does, rather than hanging below it, so
 * the composition closes on the dial's own geometry instead of on the canvas
 * edge. Derived from arcEndFraction so it tracks the sweep rather than a
 * number matched by eye.
 */
const ARC_END_Y = RING_TOP + arcEndFraction(250) * RING_BOX

const CLUSTER_CFG: ClusterCanvasConfig = {
  width: 520,
  // Cropped to where the composition actually closes: the arc's ends and the
  // lower cards' bottoms, plus a little air. The dial's box carries empty
  // space below that, which the canvas has no reason to inherit.
  height: Math.round(RING_TOP + arcEndFraction(250) * RING_BOX + 12),
  ringBox: RING_BOX,
  ringTop: RING_TOP,
  topCardW: 196,
  bottomCardW: 196,
  cardH: BOTTOM_CARD_H,
  topCardH: TOP_CARD_H,
  bottomCardH: BOTTOM_CARD_H,
  bottomCardTop: Math.round(ARC_END_Y - BOTTOM_CARD_H),
  gap: 14,
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

  const { width, height, ringBox, ringTop, topCardW, bottomCardW } = CLUSTER_CFG
  const { topCardH, bottomCardH, bottomCardTop } = CLUSTER_MASKS.geometry
  const cardStyles: CSSProperties[] = [
    { left: 0, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tl },
    { left: width - topCardW, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tr },
    { left: 0, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.bl },
    { left: width - bottomCardW, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.br },
  ]

  return (
    <div data-skin={config.skin === 'instrument' ? 'instrument' : undefined} className="h-full">
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
            gap the size of the unscaled design. */}
        <div ref={ref} className="flex w-full items-center justify-center" style={{ height: height * scale }}>
          <div
            className="relative"
            style={{ width, height, transform: `scale(${scale})`, transformOrigin: 'top center' }}
          >
            <div
              className="absolute"
              style={{ left: (width - ringBox) / 2, top: ringTop, width: ringBox, height: ringBox }}
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
                    className={`font-display text-5xl leading-none tabular-nums ${zoneTextClass(ring.zone)}`}>
                    {ring.text ?? '--'}
                  </span>
                  {/* The divisor belongs with the unit it scales, not floating
                      at the dial's foot where the notch now sits. */}
                  <span className="mt-1 text-[10px] uppercase tracking-[0.18em] text-muted-foreground">
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
                </div>
              </DialRing>
            </div>

            {config.corners.slice(0, CORNER_LAYOUT.length).map((corner, index) => (
              <CornerCard key={index} index={index} corner={corner} values={values} style={cardStyles[index]} />
            ))}
          </div>
        </div>
      </Tile>
    </div>
  )
})
