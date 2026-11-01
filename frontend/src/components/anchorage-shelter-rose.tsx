import { useMemo } from 'react'

import { GRADE_LABEL, GRADE_TOKEN, MAX_FETCH_NM, roseLabel, roseSectors } from '@/lib/anchorage-card'
import type { AnchorageCard, ShelterGrade } from '@/lib/mate-cards'
import { polar, wedgePath } from '@/lib/wind-rose'

// Each grade is drawn in a theme token, so the rose follows day and night.
const gradeFill = (grade: ShelterGrade) => `hsl(var(${GRADE_TOKEN[grade]}))`
const TRACK = 'hsl(var(--border))'
const TOKEN_TEXT = 'hsl(var(--muted-foreground))'
const CARD = 'hsl(var(--card))'

const SIZE = 240
const CENTRE = SIZE / 2
const RADIUS = 92
/** A sector is 10 degrees; a spoke fills this much of it so neighbours stay apart. */
const SPOKE_HALF_WIDTH = 4.4
/** The grade band sits just outside the outer ring, so it shows for every bearing whatever its fetch. */
const BAND_INNER = RADIUS + 3
const BAND_OUTER = RADIUS + 8
/** Ring distances are written along this bearing, clear of the N, E, S and W letters. */
const RING_LABEL_BEARING = 45
const RINGS_NM = [5, MAX_FETCH_NM]
const COMPASS: [string, number][] = [['N', 0], ['E', 90], ['S', 180], ['W', 270]]

/**
 * The shelter rose: 36 sectors of 10 degrees, one for each direction the wind
 * can come from. Colour is the grade and the spoke's length is the fetch (how
 * far the wind has run over water) up to 15 nm. A short spoke is shore close
 * by on that side. A thin band outside the ring repeats each sector's grade, so
 * a sheltered side with no spoke to see still shows.
 */
export function AnchorageShelterRose({ card }: { card: AnchorageCard }) {
  const sectors = useMemo(() => roseSectors(card), [card])
  const radius = (share: number) => RADIUS * share
  return (
    <svg role="img" aria-label={roseLabel(card)} viewBox={`0 0 ${SIZE} ${SIZE}`} className="h-auto w-full max-w-[240px] shrink-0" data-testid="anchorage-rose">
      {RINGS_NM.map((nm) => (
        <g key={nm}>
          <circle cx={CENTRE} cy={CENTRE} r={radius(nm / MAX_FETCH_NM)} fill="none" stroke={TRACK} strokeWidth={1} strokeDasharray={nm === MAX_FETCH_NM ? undefined : '2 3'} />
        </g>
      ))}
      {sectors.map((s) => (
        <path
          key={s.bearingDeg}
          d={wedgePath(CENTRE, CENTRE, 0, Math.max(radius(s.share), 2), s.bearingDeg - SPOKE_HALF_WIDTH, s.bearingDeg + SPOKE_HALF_WIDTH)}
          fill={gradeFill(s.grade)}
          fillOpacity={0.85}
          data-grade={s.grade}
          data-bearing={s.bearingDeg}
        />
      ))}
      {sectors.map((s) => (
        <path
          key={s.bearingDeg}
          d={wedgePath(CENTRE, CENTRE, BAND_INNER, BAND_OUTER, s.from, s.to)}
          fill={gradeFill(s.grade)}
          data-band-grade={s.grade}
        />
      ))}
      {/* Drawn after the spokes, with a halo in the card colour, so a long spoke
          never hides a ring's distance. Inside the ring, so the grade band
          outside it does not cover it either. */}
      {RINGS_NM.map((nm) => {
        const { x, y } = polar(CENTRE, CENTRE, radius(nm / MAX_FETCH_NM), RING_LABEL_BEARING)
        return (
          <text
            key={nm}
            x={x - 3}
            y={y + 10}
            textAnchor="end"
            fontSize="10"
            letterSpacing="0.05em"
            fill={TOKEN_TEXT}
            stroke={CARD}
            strokeWidth={3}
            paintOrder="stroke"
            data-ring-label={nm}
          >
            {nm} NM
          </text>
        )
      })}
      <circle cx={CENTRE} cy={CENTRE} r={3} fill={CARD} stroke={TOKEN_TEXT} strokeWidth={1.5} />
      {COMPASS.map(([label, bearing]) => {
        const { x, y } = polar(CENTRE, CENTRE, RADIUS + 16, bearing)
        return (
          <text key={label} x={x} y={y} fontSize="10" fill={TOKEN_TEXT} textAnchor="middle" dominantBaseline="central">
            {label}
          </text>
        )
      })}
    </svg>
  )
}

export function GradeLegend() {
  return (
    <ul aria-label="Shelter grades" className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
      {(Object.keys(GRADE_LABEL) as ShelterGrade[]).map((g) => (
        <li key={g} className="flex items-center gap-1">
          <span className="inline-block h-2 w-2 rounded-sm" style={{ background: gradeFill(g) }} aria-hidden="true" />
          {GRADE_LABEL[g]}
        </li>
      ))}
    </ul>
  )
}
