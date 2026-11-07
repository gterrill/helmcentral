import { memo } from 'react'
import { WarningNote } from '@/components/patterns'
import {
  DIAGRAM_HULL_PX,
  DIAGRAM_LEFT,
  DIAGRAM_SEABED_PX,
  DIAGRAM_WIDTH,
  hullDraftPx,
  rodeDiagramLayout,
  rodeProfile,
} from '@/lib/rode-profile'
import { radiusDisplayValue } from '@/lib/anchor-adjust'
import { depthValueText } from '@/lib/units'

export interface RodeProfileDiagramProps {
  /** The recommended rode being drawn (m). */
  rodeM: number
  /** Bow roller to seabed at the planning depth (m): depth plus bow height. */
  depthFromHawseM: number
  bowRollerHeightM: number
  /** The planning depth includes the rise to the next high. */
  tideCorrected: boolean
  /** What to say when the rode is too short for the load; null for a method whose rode can't be. */
  liftWarning: string | null
  /**
   * a = H / w (m) from the plan's catenary model, the shape of the rode. The
   * load on the chain doesn't depend on the method, so the Ratio tab's rule of
   * thumb is checked against the same load. null when there is no plan: with no
   * load there is no shape, so nothing is drawn.
   */
  catenaryParameterM: number | null
  isImperial: boolean
}

/** A chart line's colour, as the weather charts take theirs. */
const RODE_COLOUR = 'hsl(var(--chart-wind))'

/** An anchor: ring, shank, stock and flukes, about 14 px tall, standing at (x, y). */
function AnchorGlyph({ x, y }: { x: number; y: number }) {
  return (
    <g transform={`translate(${x.toFixed(1)} ${y})`} stroke="hsl(var(--foreground))" strokeWidth="1.25">
      <circle cx="0" cy="-5" r="1.6" />
      <path d="M0 -3.4 V6 M-3 -1.5 H3 M-5 3 Q0 8 5 3" />
    </g>
  )
}

const f = (n: number) => n.toFixed(1)

/**
 * A side view of the rode: seabed, water, the boat's bow and the chain between.
 * One scale serves both axes, so the picture is not exaggerated: a 5:1 rode
 * really looks five times as long as the water is deep. The cost is a flat
 * curve in deep water, which is what the chain really does.
 *
 * The figures are in the svg's aria-label, not drawn: the svg scales to the
 * sidebar, which would shrink any text in it below the text-xs floor.
 */
export const RodeProfileDiagram = memo(function RodeProfileDiagram({
  rodeM,
  depthFromHawseM,
  bowRollerHeightM,
  tideCorrected,
  liftWarning,
  catenaryParameterM,
  isImperial,
}: RodeProfileDiagramProps) {
  if (catenaryParameterM === null) {
    return <p className="mt-2 text-xs text-muted-foreground">No wind load to draw the rode against.</p>
  }
  // A recommended rode no longer than the water is deep has nothing to show.
  if (!(rodeM > depthFromHawseM)) return null
  const planned = rodeProfile({ rodeM, heightM: depthFromHawseM, parameterM: catenaryParameterM })
  if (planned === null) return <WarningNote className="mt-2">Can't draw this rode.</WarningNote>

  const reachM = planned.reachM
  const unit = isImperial ? 'ft' : 'm'
  const whole = (meters: number) => String(radiusDisplayValue(meters, isImperial))
  const tenth = (meters: number) => depthValueText(meters, isImperial)
  const lying = (meters: number) => (whole(meters) === '0' ? 'just reaching the bottom' : `${whole(meters)} ${unit} lying on the bottom`)

  const { height: H, scale, seabedY } = rodeDiagramLayout({ reachM, heightM: depthFromHawseM })
  const toX = (m: number) => DIAGRAM_LEFT + m * scale
  const toY = (m: number) => seabedY - m * scale
  const rollerX = toX(reachM)
  const rollerY = toY(depthFromHawseM)
  // The planning depth is the water at the next high when tide corrected, so
  // the surface the boat floats at is that line.
  const highWaterY = toY(Math.max(0, depthFromHawseM - bowRollerHeightM))

  const rodePath = [{ x: 0, y: 0 }, planned.touchdown, ...planned.points]
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${f(toX(p.x))},${f(toY(p.y))}`)
    .join(' ')

  const hullBottom = highWaterY + hullDraftPx(seabedY - highWaterY)
  const hull = [
    `M${f(rollerX)},${f(rollerY)}`,
    `L${f(rollerX + DIAGRAM_HULL_PX)},${f(rollerY)}`,
    `L${f(rollerX + DIAGRAM_HULL_PX - 8)},${f(hullBottom)}`,
    `L${f(rollerX + 12)},${f(hullBottom)}`,
    `Q${f(rollerX + 2)},${f(hullBottom - 2)} ${f(rollerX)},${f(rollerY)}`,
    'Z',
  ].join(' ')

  const label = [
    `Side view: ${whole(rodeM)} ${unit} of rode from the anchor to the bow roller`,
    `${tenth(depthFromHawseM)} ${unit} from the bow roller to the bottom${tideCorrected ? ' at high water' : ''}`,
    `bow roller ${tenth(bowRollerHeightM)} ${unit} above the water`,
    lying(planned.lyingM),
  ].join(', ') + '.'

  return (
    <div className="mt-2">
      <svg viewBox={`0 0 ${DIAGRAM_WIDTH} ${H}`} className="max-h-36 w-full" role="img" aria-label={label}>
        <rect x="0" y={highWaterY} width={DIAGRAM_WIDTH} height={seabedY - highWaterY} fill="hsl(var(--chart-wave) / 0.12)" />
        <rect x="0" y={seabedY} width={DIAGRAM_WIDTH} height={DIAGRAM_SEABED_PX} fill="hsl(var(--muted-foreground) / 0.25)" />

        <line
          x1="0"
          y1={highWaterY}
          x2={DIAGRAM_WIDTH}
          y2={highWaterY}
          stroke="hsl(var(--chart-wave) / 0.8)"
          strokeWidth="1"
          strokeDasharray={tideCorrected ? '4 3' : undefined}
        />

        <path d={hull} fill="hsl(var(--card))" stroke="hsl(var(--muted-foreground))" strokeWidth="1.25" strokeLinejoin="round" />
        <circle cx={rollerX} cy={rollerY} r="2.2" fill={RODE_COLOUR} />

        <g fill="none" strokeLinejoin="round" strokeLinecap="round">
          <path d={rodePath} stroke={RODE_COLOUR} strokeWidth="2" />
          <AnchorGlyph x={toX(0)} y={seabedY - 7} />
        </g>
      </svg>

      {planned.anchorLifting && liftWarning !== null && <WarningNote className="mt-1">{liftWarning}</WarningNote>}
    </div>
  )
})
