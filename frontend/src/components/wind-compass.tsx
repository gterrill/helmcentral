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

const CARDINALS = [
  { angle: 0,   label: 'N', color: '#dc2626', size: 16 },
  { angle: 90,  label: 'E', color: '#1e3a5f', size: 14 },
  { angle: 180, label: 'S', color: '#1e3a5f', size: 14 },
  { angle: 270, label: 'W', color: '#1e3a5f', size: 14 },
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
  headingTrue: number | null
  windAngleApparentDeg: number | null // 0-360, 0=bow, positive=starboard
  windSide: 'port' | 'starboard' | null
  windAngleRelativeDeg: number | null // 0-180
  windSpeedKts: number | null
}

export function WindCompass({
  headingTrue,
  windAngleApparentDeg,
  windSide,
  windAngleRelativeDeg,
  windSpeedKts,
}: WindCompassProps) {
  const arrowGradientId = useId()

  const hdg = headingTrue ?? 0
  const sideLabel  = windSide ? windSide.toUpperCase() : '—'
  const angleLabel = windAngleRelativeDeg !== null ? `${Math.round(windAngleRelativeDeg)}°` : '—'
  const speedLabel = windSpeedKts !== null ? String(Math.round(windSpeedKts)) : '—'

  // Accumulate the arrow's rotation along the shortest path rather than the
  // raw 0-360 value, so the CSS transition doesn't spin the long way around
  // when the angle crosses the 0°/360° wrap.
  const unwrappedAngleRef = useRef<number | null>(null)
  let arrowRotation: number | null = null
  if (windAngleApparentDeg !== null) {
    const prev = unwrappedAngleRef.current
    arrowRotation = prev === null
      ? windAngleApparentDeg
      : prev + (((windAngleApparentDeg - prev + 540) % 360) - 180)
  }
  unwrappedAngleRef.current = arrowRotation

  // Bow-indicator triangle — downward-pointing, fixed at 12 o'clock
  const bowPts = [
    `${CX},${CY - RM + 1}`,
    `${CX - 7},${CY - RO + 3}`,
    `${CX + 7},${CY - RO + 3}`,
  ].join(' ')

  return (
    <svg viewBox={`0 0 ${V} ${V}`} width="100%" height="100%" aria-label="Wind compass">
      {/* ── background ─────────────────────────────────────────────── */}
      <circle cx={CX} cy={CY} r={RO} fill="hsl(var(--card))" />
      <circle cx={CX} cy={CY} r={RO} fill="none" stroke="hsl(var(--border))" strokeWidth="1.5" />
      <circle cx={CX} cy={CY} r={RM - 2} fill="none" stroke="hsl(var(--border))" strokeWidth="0.5" opacity="0.4" />

      {/* ── tick marks — rotate with heading ───────────────────────── */}
      {Array.from({ length: 36 }, (_, i) => {
        const geo     = i * 10
        const isMajor = geo % 30 === 0
        const sa      = geo - hdg
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

      {/* ── degree labels — rotate with heading, text stays upright ── */}
      {NUM_LABELS.map(geo => {
        const [x, y] = pt(geo - hdg, RL)
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

      {/* ── cardinal letters — rotate with heading, text stays upright */}
      {CARDINALS.map(({ angle, label, color, size }) => {
        const [x, y] = pt(angle - hdg, RL)
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

      {/* ── bow indicator — fixed blue triangle at 12 o'clock ──────── */}
      <polygon points={bowPts} fill="#3b82f6" />

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
              <stop offset="0%"   stopColor="#d97706" stopOpacity="0.08" />
              <stop offset="55%"  stopColor="#d97706" stopOpacity="0.78" />
              <stop offset="100%" stopColor="#92400e" stopOpacity="0.95" />
            </linearGradient>
          </defs>
          <path d={arrowD} fill={`url(#${arrowGradientId})`} />
        </g>
      )}

      {/* ── center text ─────────────────────────────────────────────── */}
      <text
        x={CX} y={CY - 22}
        textAnchor="middle" dominantBaseline="central"
        fontSize="12" fontWeight="700"
        fill="#2563eb"
        fontFamily="ui-sans-serif, system-ui, sans-serif"
        letterSpacing="1.5"
      >
        {sideLabel} {angleLabel}
      </text>

      <text
        x={CX} y={CY + 10}
        textAnchor="middle" dominantBaseline="central"
        fontSize="54" fontWeight="700"
        fill="#d97706"
        fontFamily="ui-sans-serif, system-ui, sans-serif"
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
  )
}
