import { useId, useRef } from 'react'

const V = 280
const CX = 140
const CY = 140

const RO = 130   // outer ring radius
const RM = 117   // major tick inner (13 px deep)
const Rm = 124   // minor tick inner (6 px deep)
const RL = 96    // label radius
const RI = 88    // inner clear circle
const RW = 78    // wind arrow tip

const D2R = Math.PI / 180
function pt(angleDeg: number, r: number): [number, number] {
  const a = angleDeg * D2R
  return [CX + r * Math.sin(a), CY - r * Math.cos(a)]
}

// True north is the one cardinal worth picking out of the ring, so it takes
// the SVG-text "emphasis" token; the other three read as ordinary ring
// labels, same as the degree numbers (SVG Follows the DOM rule).
const CARDINALS = [
  { angle: 0,   label: 'N', color: 'hsl(var(--primary))', size: 16 },
  { angle: 90,  label: 'E', color: 'hsl(var(--muted-foreground))', size: 14 },
  { angle: 180, label: 'S', color: 'hsl(var(--muted-foreground))', size: 14 },
  { angle: 270, label: 'W', color: 'hsl(var(--muted-foreground))', size: 14 },
] as const

const NUM_LABELS = [30, 60, 120, 150, 210, 240, 300, 330]

// Arrow pointing "up" — tip at (CX, CY-RW), base near center
const TIP_Y   = CY - RW        // 62
const HEAD_Y  = TIP_Y + 22     // 84  — arrowhead base

const arrowD = [
  `M ${CX} ${TIP_Y}`,
  `L ${CX + 9} ${HEAD_Y}`,
  `L ${CX + 2.5} ${HEAD_Y}`,
  `L ${CX + 2.5} ${CY - 5}`,
  `L ${CX - 2.5} ${CY - 5}`,
  `L ${CX - 2.5} ${HEAD_Y}`,
  `L ${CX - 9} ${HEAD_Y}`,
  'Z',
].join(' ')

interface WindCompassProps {
  /**
   * Degrees subtracted from every tick/label/cardinal's geographic bearing
   * before it's placed on the ring — headingTrue in Course Up (the ring
   * rotates under the fixed bow), 0 in North Up (the ring never rotates and
   * true north always renders at 12 o'clock). WindTile computes this; the
   * mode/orientation logic itself lives there, not here.
   */
  ringRotationDeg: number
  /**
   * Where the bow triangle sits, in the same "degrees clockwise from the
   * ring's own 12 o'clock" frame as arrowAngleDeg below — always 0 in Course
   * Up (the bow is fixed at the top by definition), headingTrue in North Up.
   * null hides the bow marker entirely rather than guessing a heading North
   * Up has no real value for.
   */
  bowRotationDeg: number | null
  /**
   * Where the wind arrow points, in the same frame as bowRotationDeg — the
   * bow-relative wind angle in Course Up, the wind's absolute compass
   * bearing in North Up. null hides the arrow (e.g. no reading for the
   * active mode, or North Up with no heading to convert apparent wind by).
   */
  arrowAngleDeg: number | null
  windSpeedKts: number | null
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null // 0-180
  /** Which reading is being shown, for the screen-reader summary's wording. */
  kind: 'apparent' | 'true'
}

/**
 * The actual readings, for a screen reader. The SVG below carries the same
 * numbers as `<text>` children, but SVG text accessibility is inconsistent
 * across screen readers — this is the one deterministic source, so the SVG
 * itself is `aria-hidden` rather than relied on.
 */
function accessibleSummary({ windSide, windAngleRelativeDeg, windSpeedKts, kind }: Pick<WindCompassProps, 'windSide' | 'windAngleRelativeDeg' | 'windSpeedKts' | 'kind'>): string {
  const speedText = windSpeedKts !== null ? `${Math.round(windSpeedKts)} knots` : 'unknown'
  const sideWord = windSide === 'starboard' ? 'starboard' : windSide === 'port' ? 'port' : null
  const angleText = windAngleRelativeDeg !== null ? `${Math.round(windAngleRelativeDeg)}°` : null
  const relativeText = sideWord && angleText ? `${angleText} off the bow, ${sideWord} side` : 'relative angle unknown'
  const kindLabel = kind === 'true' ? 'True' : 'Apparent'

  return `Wind compass. ${kindLabel} wind speed ${speedText}. ${relativeText}.`
}

/**
 * Accumulates a rotation along the shortest path rather than the raw 0-360
 * value, so a CSS transition doesn't spin the long way around when the angle
 * crosses the 0°/360° wrap. Shared by the wind arrow and (North Up) the bow
 * marker — both are `g` elements rotated the same way, so both need the same
 * unwrap treatment; each call gets its own ref/accumulator.
 */
function useShortestPathRotation(angleDeg: number | null): number | null {
  const unwrappedRef = useRef<number | null>(null)
  let rotation: number | null = null
  if (angleDeg !== null) {
    const prev = unwrappedRef.current
    if (prev === null) {
      rotation = angleDeg
    } else {
      // True modulo, not JS's `%` (which is a truncating remainder and
      // returns a negative result for a negative dividend). `prev`
      // accumulates every step (it is `prev + delta`, not `angleDeg`
      // itself), so after enough same-direction steps it can end up more
      // than 540° ahead of the next raw angle, at which point
      // `angleDeg - prev + 540` goes negative and a plain `% 360` no
      // longer lands in [0, 360) - it stays negative, which used to send
      // delta far outside [-180, 180] and spin the marker a full turn
      // backwards. Reducing `angleDeg - prev` to [0, 360) first with an
      // explicit `+ 360) % 360`-style double mod, then applying the same
      // `+ 540) % 360) - 180` shift, keeps delta correct at any
      // accumulated magnitude.
      const delta = ((((angleDeg - prev) % 360) + 540) % 360) - 180
      rotation = prev + delta
    }
  }
  unwrappedRef.current = rotation
  return rotation
}

export function WindCompass({
  ringRotationDeg,
  bowRotationDeg,
  arrowAngleDeg,
  windSide,
  windAngleRelativeDeg,
  windSpeedKts,
  kind,
}: WindCompassProps) {
  const arrowGradientId = useId()

  const sideLabel  = windSide === 'starboard' ? 'S' : windSide === 'port' ? 'P' : '—'
  const angleLabel = windAngleRelativeDeg !== null ? `${Math.round(windAngleRelativeDeg)}°` : '—'
  const speedLabel = windSpeedKts !== null ? String(Math.round(windSpeedKts)) : '—'

  const arrowRotation = useShortestPathRotation(arrowAngleDeg)
  const bowRotation = useShortestPathRotation(bowRotationDeg)

  // Bow-indicator triangle — downward-pointing, drawn fixed at 12 o'clock in
  // its own local coordinates; bowRotation (above) carries it to wherever it
  // actually belongs (always 0/unrotated in Course Up).
  const bowPts = [
    `${CX},${CY - RM + 1}`,
    `${CX - 7},${CY - RO + 3}`,
    `${CX + 7},${CY - RO + 3}`,
  ].join(' ')

  return (
    <>
      <span className="sr-only" data-testid="wind-compass-summary">
        {accessibleSummary({ windSide, windAngleRelativeDeg, windSpeedKts, kind })}
      </span>
      <svg viewBox={`0 0 ${V} ${V}`} width="100%" height="100%" aria-hidden="true" data-testid="wind-compass-svg">
        {/* ── background ─────────────────────────────────────────────── */}
        <circle cx={CX} cy={CY} r={RO} fill="hsl(var(--card))" />
        <circle cx={CX} cy={CY} r={RO} fill="none" stroke="hsl(var(--border))" strokeWidth="1.5" />
        <circle cx={CX} cy={CY} r={RM - 2} fill="none" stroke="hsl(var(--border))" strokeWidth="0.5" opacity="0.4" />

        {/* ── tick marks — rotate with ringRotationDeg ───────────────── */}
        {Array.from({ length: 36 }, (_, i) => {
          const geo     = i * 10
          const isMajor = geo % 30 === 0
          const sa      = geo - ringRotationDeg
          const [x1, y1] = pt(sa, RO - 0.5)
          const [x2, y2] = pt(sa, isMajor ? RM : Rm)
          return (
            <line
              key={geo}
              x1={x1} y1={y1} x2={x2} y2={y2}
              stroke={isMajor ? 'hsl(var(--muted-foreground))' : 'hsl(var(--border))'}
              strokeWidth={isMajor ? 1.5 : 0.75}
            />
          )
        })}

        {/* ── degree labels — rotate with ringRotationDeg, text stays upright */}
        {NUM_LABELS.map(geo => {
          const [x, y] = pt(geo - ringRotationDeg, RL)
          return (
            <text
              key={geo}
              x={x} y={y}
              textAnchor="middle" dominantBaseline="central"
              fontSize="10" fill="hsl(var(--muted-foreground))"
              fontFamily="ui-monospace, SFMono-Regular, monospace"
            >
              {String(geo).padStart(3, '0')}
            </text>
          )
        })}

        {/* ── cardinal letters — rotate with ringRotationDeg, text stays upright */}
        {CARDINALS.map(({ angle, label, color, size }) => {
          const [x, y] = pt(angle - ringRotationDeg, RL)
          return (
            <text
              key={angle}
              x={x} y={y}
              textAnchor="middle" dominantBaseline="central"
              fontSize={size} fontWeight="700"
              fill={color}
              fontFamily="ui-sans-serif, system-ui, sans-serif"
            >
              {label}
            </text>
          )
        })}

        {/* ── inner clear circle ──────────────────────────────────────── */}
        <circle cx={CX} cy={CY} r={RI} fill="hsl(var(--card))" />
        <circle cx={CX} cy={CY} r={RI} fill="none" stroke="hsl(var(--border))" strokeWidth="0.75" />

        {/* ── bow indicator — Signal Blue triangle, fixed at 12 o'clock in
            Course Up (bowRotation is always 0 then) or swept around the ring
            to the vessel's heading in North Up. Hidden (not drawn at 0) when
            bowRotationDeg is null — North Up with no heading to place it by. */}
        {bowRotation !== null && (
          <g
            style={{
              transform: `rotate(${bowRotation}deg)`,
              transformOrigin: `${CX}px ${CY}px`,
              transition: 'transform 650ms ease-out',
            }}
          >
            <polygon points={bowPts} fill="hsl(var(--primary))" />
          </g>
        )}

        {/* ── wind arrow — gradient defined inside the rotating group ───
            Rotation is driven by the CSS `transform` property (not the SVG
            `transform` attribute) so the `transition` below actually animates
            it — browsers don't consistently transition attribute-driven
            rotation on SVG elements. */}
        {arrowRotation !== null && (
          <g
            style={{
              transform: `rotate(${arrowRotation}deg)`,
              transformOrigin: `${CX}px ${CY}px`,
              transition: 'transform 650ms ease-out',
            }}
          >
            <defs>
              {/* userSpaceOnUse coords are in the rotated space → gradient rotates with arrow */}
              <linearGradient
                id={arrowGradientId}
                gradientUnits="userSpaceOnUse"
                x1={CX} y1={CY - 5}
                x2={CX} y2={TIP_Y}
              >
                <stop offset="0%"   stopColor="hsl(var(--gauge-primary))" stopOpacity="0.08" />
                <stop offset="55%"  stopColor="hsl(var(--gauge-primary))" stopOpacity="0.78" />
                <stop offset="100%" stopColor="hsl(var(--gauge-primary))" stopOpacity="1" />
              </linearGradient>
            </defs>
            <path d={arrowD} fill={`url(#${arrowGradientId})`} />
          </g>
        )}

        {/* ── center text ─────────────────────────────────────────────── */}
        <text
          x={CX} y={CY - 22}
          textAnchor="middle" dominantBaseline="central"
          fontSize="16" fontWeight="700"
          fill="hsl(var(--gauge-primary))"
          fontFamily="var(--font-display), monospace"
          style={{ fontVariantNumeric: 'tabular-nums' }}
          letterSpacing="1.5"
          data-testid="wind-side-angle-text"
        >
          {sideLabel}{angleLabel}
        </text>

        <text
          x={CX} y={CY + 10}
          textAnchor="middle" dominantBaseline="central"
          fontSize="54" fontWeight="700"
          fill="hsl(var(--gauge-primary))"
          fontFamily="var(--font-display), monospace"
          style={{ fontVariantNumeric: 'tabular-nums' }}
          data-testid="wind-speed-text"
        >
          {speedLabel}
        </text>

        <text
          x={CX} y={CY + 44}
          textAnchor="middle" dominantBaseline="central"
          fontSize="12" letterSpacing="3"
          fill="hsl(var(--muted-foreground))"
          fontFamily="ui-sans-serif, system-ui, sans-serif"
        >
          kts
        </text>
      </svg>
    </>
  )
}
