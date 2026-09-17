import { Gauge as GaugeIcon, Settings2 } from 'lucide-react'

import { CoolantIcon, ExhaustIcon, GearboxIcon } from '@/components/ui/telltale-icons'
import { memo, type CSSProperties } from 'react'

import { Button } from '@/components/ui/button'
import { DialRing, zoneColor } from '@/components/ui/dial-ring'
import { Tile } from '@/components/ui/tile'
import { CLUSTER_ICONS, iconForSlot } from '@/lib/cluster-icons'
import { reading, zoneFor, zoneTextClass } from '@/lib/cluster-readings'
import { CLUSTER_CANVAS, CLUSTER_RAIL, clusterDesignWidth, computeCornerMasks, useFitScale } from '@/lib/cluster-canvas'
import { FuelRail } from '@/components/ui/fuel-rail'
import type { ClusterCorner, EngineClusterConfig, GaugeWidgetConfig } from '@/lib/dashboard-widgets'
import { majorStepFor } from '@/components/gauge-tile'
import { severityTextClass, worstZoneState, type ZoneState } from '@/lib/severity'
import { formatDataAge } from '@/lib/staleness'

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
const CLUSTER_MASKS = computeCornerMasks(CLUSTER_CANVAS)

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
        return <rect key={index} x={from} y="1.5" width={Math.max(0, to - from)} height="1" fill={zoneColor(zone.state)} />
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

function CornerCard({ corner, values, ages, style, index }: {
  corner: ClusterCorner
  values: Record<string, number | null>
  ages?: Record<string, number | null>
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
        const { text, unit, converted, zone, stale, age } = reading(row, values, ages)
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
              {/* A frozen row carries its own badge (ADR 0083) rather than
                  staling the whole card: one dead sensor among several must
                  not blank readings that are still live. */}
              {stale && (
                <span className="shrink-0 rounded-xs border border-amber-500/40 bg-amber-500/10 px-1.5 py-0.5 text-[10px] leading-none text-amber-600 dark:text-amber-400">
                  Stale {formatDataAge(age)}
                </span>
              )}
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
  // A reading with no bands configured, or one sitting inside its normal
  // band, still means the engine is turning and nothing has flagged it,
  // which is what green says - the telltale's own fallback rather than
  // severityTextClass's caller-supplied default. Above the band the profile
  // calls normal, but this engine's warn and alarm thresholds are null in
  // the profile - nobody has filled them in from the manual yet - so every
  // reading over the normal band lands on `outside`, which severityTextClass
  // maps to warn's amber. Red would claim an overheat the configuration
  // cannot actually know about.
  return severityTextClass(state, 'text-emerald-500')
}

/**
 * The same telltale reading, translated to the tile-edge vocabulary (ADR
 * 0081) instead of a text class. A telltale's own colour logic above already
 * decides "past alarm", "in between" and "normal" - this reuses that same
 * `state`/`value` pair rather than asking the zones a second question, so the
 * two can never disagree about which tier a telltale is in.
 */
function telltaleZoneState(state: ReturnType<typeof zoneFor>, value: number | null): ZoneState | null {
  if (value === null) return null
  if (state === 'alarm' || state === 'emergency') return 'alarm'
  if (state === null || state === 'normal') return 'normal'
  return 'warn' // 'outside', 'warn' and 'alert' all read as the telltale's amber tier.
}

/**
 * The telltale row, under the hours notch at the dial's foot. It sits in the
 * wedge the 250-degree sweep leaves at the bottom, so it costs the composition
 * nothing and puts the lights where the eye already is.
 */
function Telltales({ slots, values, ages }: {
  slots: GaugeWidgetConfig[]
  values: Record<string, number | null>
  ages?: Record<string, number | null>
}) {
  return (
    <div data-testid="cluster-telltales" className="mt-1.5 flex items-center justify-center gap-3">
      {slots.map((slot, index) => {
        // A stale telltale reads exactly as a never-reported one (grey,
        // ADR 0083): reading() already blanks converted/zone when stale, and
        // telltaleClass already renders a null value grey.
        const { text, converted, zone } = reading(slot, values, ages)
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
  /** Age in seconds behind each bound path (ADR 0083); absent is unknown. */
  ages?: Record<string, number | null>
  editing: boolean
  onConfigure: () => void
}

export const EngineClusterTile = memo(function EngineClusterTile({
  config, values, ages, editing, onConfigure,
}: EngineClusterTileProps) {
  // The rail widens only the design the canvas is scaled against. The body
  // keeps its own 520-wide coordinate space, which is what every corner mask
  // was computed in.
  const fuel = config.fuel
  const designWidth = clusterDesignWidth(Boolean(fuel))
  const bodyLeft = fuel?.side === 'left' ? CLUSTER_RAIL.width + CLUSTER_RAIL.gap : 0
  const railLeft = fuel?.side === 'left' ? 0 : CLUSTER_CANVAS.width + CLUSTER_RAIL.gap

  const [ref, scale] = useFitScale(designWidth)
  const title = config.title.trim() || 'Engine'

  const ring = reading(config.ring, values, ages)
  const centre = reading(config.centre, values, ages)
  const ringMin = config.ring.min ?? 0
  const ringMax = config.ring.max ?? 100

  const telltales = config.telltales ?? []
  const { width, height, ringBox, ringTop, topCardW, bottomCardW } = CLUSTER_CANVAS
  const { topCardH, bottomCardH, bottomCardTop } = CLUSTER_MASKS.geometry
  // The telltales live inside the dial now, so the canvas is exactly the block
  // of boxes again and the disc still spans it.
  const canvasH = height

  const cornerReadings = config.corners.flatMap((corner) => corner.rows.map((row) => reading(row, values, ages)))
  const telltaleReadings = telltales.map((slot) => reading(slot, values, ages))

  // The tile edge carries the worst of everything the cluster reads (ADR
  // 0081): the ring, the centre, every corner row, and the telltales in
  // their own vocabulary via telltaleZoneState above. reading() already
  // blanks a stale slot's zone to null (ADR 0083), so a frozen reading drops
  // out of this worst-of the same way an absent one always has.
  const state = worstZoneState([
    ring.zone,
    centre.zone,
    ...cornerReadings.map((r) => r.zone),
    ...telltaleReadings.map((r) => telltaleZoneState(r.zone, r.converted)),
  ])

  // The tile itself only goes stale once every slot whose age is actually
  // known has frozen (ADR 0083) -- the shape a dead engine feed takes, where
  // the ring, the corners and the telltales all stop together. A cluster
  // with no known ages at all is neither fresh nor stale.
  const allReadings = [ring, centre, ...cornerReadings, ...telltaleReadings]
  const knownAges = allReadings.filter((r) => r.age !== null)
  const tileStale = knownAges.length > 0 && knownAges.every((r) => r.stale)
  const staleLabel = tileStale ? formatDataAge(Math.max(...knownAges.map((r) => r.age as number))) : undefined
  const cardStyles: CSSProperties[] = [
    { left: 0, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tl },
    { left: width - topCardW, top: 0, width: topCardW, height: topCardH, ...CLUSTER_MASKS.tr },
    { left: 0, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.bl },
    { left: width - bottomCardW, top: bottomCardTop, width: bottomCardW, height: bottomCardH, ...CLUSTER_MASKS.br },
  ]

  return (
    <Tile
      title={title}
      state={state}
      stale={tileStale}
      staleLabel={staleLabel}
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
          style={{ width: designWidth, height: canvasH, transform: `scale(${scale})`, transformOrigin: 'top center' }}
        >
          {fuel && (
            <div className="absolute" style={{ left: railLeft, top: 0 }}>
              <FuelRail config={fuel} values={values} height={canvasH} />
            </div>
          )}

          {/* The body keeps the canvas's own width whatever the rail does, so
              the cards sit at the offsets their masks were cut for. */}
          <div data-cluster-body="" className="absolute" style={{ left: bodyLeft, top: 0, width, height: canvasH }}>
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
              {/* The centre belongs to the ring reading alone.
                  Two groups, not one stack: the reading is centred on the dial
                  and the hours notch and telltales hang off its foot. Stacked
                  together they came to half the dial's height, which pushed the
                  reading up off the middle and straight through the scale
                  numbers - the widest part of the readout meeting the narrowest
                  part of the face. */}
              <div data-testid="cluster-centre" className="flex flex-col items-center">
                <span data-testid="cluster-centre-value"
                  className={`font-display text-4xl leading-none tracking-tight tabular-nums ${zoneTextClass(ring.zone)}`}>
                  {ring.text ?? '--'}
                </span>
                {/* A rule between the reading and what it is measured in. It
                    is what separates an instrument face from a number with a
                    caption under it, and it costs one div. */}
                <div className="mt-1.5 h-0.5 w-[68px] bg-foreground/25" />
                {/* The divisor belongs with the unit it scales, not floating
                    at the dial's foot where the notch now sits. */}
                <span className="mt-1.5 text-xs font-semibold uppercase tracking-[0.16em] text-foreground">
                  {config.ring.label.trim() || ring.unit}
                  {config.ring.labelDivisor ? ` x${config.ring.labelDivisor}` : ''}
                </span>
              </div>

              {/* The foot: the hours notch and the telltale row, in the wedge
                  the 250-degree sweep leaves at the bottom. Pinned there rather
                  than stacked under the reading, so neither group moves when
                  the other changes height. 15% clears the rim at the width the
                  row runs to. */}
              <div data-testid="cluster-foot"
                className="absolute bottom-[15%] left-0 right-0 flex flex-col items-center">
                {/* Smaller than the reading it sits under by more than one
                    step. Hours are a number you look up, not one you monitor,
                    and at text-sm the pill crowded the unit caption above it. */}
                <span data-testid="cluster-notch"
                  className="flex items-center gap-1.5 rounded-full border bg-card/90 px-2 py-0.5">
                  <SlotIcon name={config.centreIcon} slot={config.centre} className="size-3 shrink-0 text-gauge-secondary" />
                  <span className="font-display text-xs leading-none tabular-nums text-gauge-secondary">
                    {centre.text ?? '--'}
                  </span>
                  {centre.unit && <span className="text-[9px] text-muted-foreground">{centre.unit}</span>}
                </span>

                {telltales.length > 0 && <Telltales slots={telltales} values={values} ages={ages} />}
              </div>
            </DialRing>
          </div>

          {config.corners.slice(0, CORNER_LAYOUT.length).map((corner, index) => (
            <CornerCard key={index} index={index} corner={corner} values={values} ages={ages} style={cardStyles[index]} />
          ))}
          </div>
        </div>
      </div>
    </Tile>
  )
})
