import { Wind } from 'lucide-react'
import { useMemo } from 'react'

import type { WindRoseCard } from '@/lib/mate-cards'
import { formatRange } from '@/lib/telemetry-chart'
import { polar, ringTicks, wedgePath, windRoseModel } from '@/lib/wind-rose'

// One hue, light to strong with the speed band. The hue is the same chart token
// the wind forecast uses, so it follows the theme; strength is its opacity.
const PETAL = 'hsl(var(--chart-wind))'
const TOKEN_TEXT = 'hsl(var(--muted-foreground))'
const CARD = 'hsl(var(--card))'

const SIZE = 260
const CENTRE = SIZE / 2
const RADIUS = 92
/** How much of its sector a petal fills, so neighbours stay apart. */
const PETAL_FILL = 0.84

function pctText(v: number): string {
  return `${v >= 1 ? Math.round(v) : Number(v.toFixed(1))}%`
}

function bandOpacity(i: number, n: number): number {
  return n <= 1 ? 1 : 0.28 + (0.72 * i) / (n - 1)
}

function whenText(iso: string, timeZone: string): string {
  const ps = new Intl.DateTimeFormat('en-GB', { timeZone, weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(
    new Date(iso),
  )
  const get = (t: string) => ps.find((p) => p.type === t)?.value ?? ''
  return `${get('weekday')} ${get('day')} ${get('month')} ${get('hour')}:${get('minute')}`
}

function averages(minutes: number): string {
  return minutes >= 60 && minutes % 60 === 0 ? `${minutes / 60}-hour averages` : `${minutes}-minute averages`
}

/**
 * The wind rose under a Mate reply (ADR 0221): petals for the compass sectors
 * the wind blew from, stacked outward by speed band. The server counted the
 * logged true wind; this draws the counts and states what they leave out (calm,
 * wind with no steady direction).
 */
export function AssistantWindRoseCard({ card }: { card: WindRoseCard }) {
  const model = useMemo(() => windRoseModel(card), [card])
  const rings = ringTicks(model.maxPct)
  const outer = rings.length > 0 ? rings[rings.length - 1] : 1
  const radius = (pct: number) => (RADIUS * pct) / outer
  const half = (360 / card.sectors / 2) * PETAL_FILL
  const prevailing = model.prevailing
  const summary = prevailing ? `mostly from ${prevailing.compass} (${pctText(prevailing.pct)} of the time)` : 'no steady direction'

  return (
    <section
      aria-label="Wind rose"
      data-testid="assistant-wind-rose-card"
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <Wind className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Wind rose</span>
        <span className="min-w-0 flex-1 line-clamp-2 text-sm font-medium text-foreground" title={card.title}>
          {card.title}
        </span>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{formatRange(card.range.from, card.range.to, card.timezone)}</span>
      </div>

      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role="img"
        aria-label={`Wind rose, ${card.title}, ${summary}`}
        className="mx-auto h-auto w-full max-w-xs"
      >
        {rings.map((t) => (
          <circle key={t} cx={CENTRE} cy={CENTRE} r={radius(t)} fill="none" stroke="hsl(var(--border))" strokeDasharray="2 2" />
        ))}
        <line x1={CENTRE} y1={CENTRE - RADIUS} x2={CENTRE} y2={CENTRE + RADIUS} stroke="hsl(var(--border))" />
        <line x1={CENTRE - RADIUS} y1={CENTRE} x2={CENTRE + RADIUS} y2={CENTRE} stroke="hsl(var(--border))" />

        {model.sectors.map((s) =>
          s.bands.map((b, bi) => {
            if (b.pct <= 0) return null
            return (
              <path
                key={`${s.index}-${bi}`}
                data-testid="rose-wedge"
                d={wedgePath(CENTRE, CENTRE, radius(b.from), radius(b.to), s.centre - half, s.centre + half)}
                fill={PETAL}
                fillOpacity={bandOpacity(bi, card.bins.length)}
                stroke={CARD}
                strokeWidth={0.75}
              >
                <title>{`${s.compass}, ${model.bandLabels[bi]}: ${pctText(b.pct)}`}</title>
              </path>
            )
          }),
        )}

        {rings.map((t) => {
          const at = polar(CENTRE, CENTRE, radius(t), 360 / card.sectors / 2)
          return (
            <text key={t} x={at.x + 2} y={at.y} fontSize="10" fill={TOKEN_TEXT} stroke={CARD} strokeWidth={3} paintOrder="stroke">
              {pctText(t)}
            </text>
          )
        })}

        {(
          [
            ['N', 0],
            ['E', 90],
            ['S', 180],
            ['W', 270],
          ] as const
        ).map(([label, bearing]) => {
          const at = polar(CENTRE, CENTRE, RADIUS + 12, bearing)
          return (
            <text key={label} x={at.x} y={at.y} fontSize="10" fill={TOKEN_TEXT} textAnchor="middle" dominantBaseline="central">
              {label}
            </text>
          )
        })}
      </svg>

      <ul data-testid="rose-legend" className="flex min-w-0 flex-wrap gap-x-3 gap-y-1" aria-label="Wind speed">
        {model.bandLabels.map((label, i) => (
          <li key={label} className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
            <span
              className="h-2.5 w-2.5 shrink-0 rounded-sm"
              style={{ background: PETAL, opacity: bandOpacity(i, card.bins.length) }}
              aria-hidden="true"
            />
            <span className="min-w-0 truncate tabular-nums">{label}</span>
          </li>
        ))}
      </ul>

      <p data-testid="rose-facts" className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {prevailing ? (
          <span className="font-medium text-foreground">{`Mostly from ${prevailing.compass}, ${pctText(prevailing.pct)} of the time`}</span>
        ) : (
          <span className="font-medium text-foreground">No steady wind direction</span>
        )}
        {card.calm > 0 && <span className="tabular-nums">{`Calm ${pctText(model.calmPct)} (under ${card.calmBelow} ${card.speedUnit})`}</span>}
        {card.variable > 0 && <span className="tabular-nums">{`Variable ${pctText(model.variablePct)}`}</span>}
        {card.meanSpeed !== null && <span className="tabular-nums">{`Mean ${card.meanSpeed} ${card.speedUnit}`}</span>}
        {card.strongest && (
          <span className="tabular-nums">{`Strongest ${card.strongest.speed} ${card.speedUnit}, ${whenText(card.strongest.at, card.timezone)}`}</span>
        )}
        <span>{averages(card.bucketMinutes)}</span>
      </p>
      {card.unpaired > 0 && (
        <p className="min-w-0 text-xs text-muted-foreground">{`${card.unpaired} buckets had direction or speed but not both, and are left out.`}</p>
      )}
    </section>
  )
}
