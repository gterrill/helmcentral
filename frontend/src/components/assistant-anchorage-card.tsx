import { Anchor } from 'lucide-react'
import { memo, useState, type ReactNode } from 'react'

import { AssistantAnchorageMap } from '@/components/assistant-anchorage-map'
import { AnchorageShelterRose, GradeLegend } from '@/components/anchorage-shelter-rose'
import {
  dateTimeText,
  directionText,
  footerLines,
  GRADE_LABEL,
  GRADE_TONE,
  groupMoorings,
  kn,
  lengthText,
  METRES_PER_NM,
  restrictedLine,
  timeText,
  waveText,
  windText,
  type BadgeTone,
} from '@/lib/anchorage-card'
import { useAppConfig } from '@/hooks/use-app-config'
import { distanceText, humanise } from '@/lib/marina-card'
import type { AnchorageCard, ShelterGrade } from '@/lib/mate-cards'
import { formatRowDate } from '@/lib/passage-dates'
import { cn } from '@/lib/utils'

const TONE: Record<BadgeTone, string> = {
  critical: 'border-red-500/40 bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-300',
  warning: 'border-amber-500/40 bg-amber-50 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  good: 'border-primary/40 bg-primary/10 text-foreground',
  neutral: 'border-border bg-card text-foreground',
  muted: 'border-border bg-muted/40 text-muted-foreground',
}

function GradeBadge({ grade }: { grade: ShelterGrade }) {
  return <span className={cn('inline-block whitespace-nowrap rounded border px-1.5 py-0.5 text-xs', TONE[GRADE_TONE[grade]])}>{GRADE_LABEL[grade]}</span>
}

/** A section that could not be checked says why, in the words the tool gave. It shows nothing else. */
function SectionError({ text, testId }: { text: string; testId: string }) {
  return (
    <p data-testid={testId} className="min-w-0 rounded border border-amber-500/40 bg-amber-50 px-2 py-1 text-xs text-amber-800 dark:bg-amber-950 dark:text-amber-300">
      {text}
    </p>
  )
}

/** A titled section; with an `error` it shows that in place of its children. */
function Block({ title, testId, error, children }: { title: string; testId: string; error?: string; children: ReactNode }) {
  return (
    <section aria-label={title} data-testid={testId} className="flex min-w-0 flex-col gap-1.5 border-t border-border pt-2">
      <h4 className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{title}</h4>
      {error ? <SectionError text={error} testId={`${testId}-error`} /> : children}
    </section>
  )
}

function ShelterBlock({ card }: { card: AnchorageCard }) {
  return (
    <Block title="Shelter" testId="anchorage-shelter">
      <div className="flex min-w-0 flex-col items-center gap-3 sm:flex-row sm:items-start">
        <AnchorageShelterRose card={card} />
        <div className="flex min-w-0 flex-1 flex-col gap-2 text-xs">
          <ul aria-label="Shelter arcs" className="flex min-w-0 flex-col gap-1">
            {card.shelter.arcs.map((a, i) => (
              <li key={i} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                <GradeBadge grade={a.grade} />
                <span className="min-w-0 text-foreground">{a.text}</span>
              </li>
            ))}
          </ul>
          {card.shelter.insideReef && (
            <p className="text-muted-foreground">Inside {card.shelter.reefName || 'a reef'}: shelter depends on the tide.</p>
          )}
          <GradeLegend />
          <p className="text-muted-foreground">Directions are where the wind comes from. Spoke length is the fetch up to 15 nm. An estimate from charted shoreline only.</p>
        </div>
      </div>
    </Block>
  )
}

function ForecastBlock({ card }: { card: AnchorageCard }) {
  const { ui } = useAppConfig()
  const f = card.forecast
  return (
    <Block title="Forecast over the stay" testId="anchorage-forecast" error={f.error}>
      <div className="min-w-0 overflow-x-auto">
        <table className="w-full min-w-[22rem] text-xs tabular-nums">
          <caption className="sr-only">Three-hourly wind, shelter and waves from arrival to departure, in the anchorage's local time</caption>
          <thead>
            <tr className="text-left text-[10px] uppercase tracking-wider text-muted-foreground">
              <th scope="col" className="py-0.5 pr-2 font-medium">Time</th>
              <th scope="col" className="py-0.5 pr-2 font-medium">Wind kn</th>
              <th scope="col" className="py-0.5 pr-2 font-medium">From</th>
              <th scope="col" className="py-0.5 pr-2 font-medium">Shelter</th>
              <th scope="col" className="py-0.5 font-medium">Waves</th>
            </tr>
          </thead>
          <tbody>
            {f.rows.map((r) => (
              <tr key={r.time} className="border-t border-border/60">
                <td className="whitespace-nowrap py-0.5 pr-2 text-foreground">{timeText(r.time, card.timezone)}</td>
                <td className="whitespace-nowrap py-0.5 pr-2">{windText(r)}</td>
                <td className="whitespace-nowrap py-0.5 pr-2">{directionText(r)}</td>
                <td className="py-0.5 pr-2">{r.shelter === '' ? <span className="text-muted-foreground">--</span> : <GradeBadge grade={r.shelter} />}</td>
                <td className="whitespace-nowrap py-0.5">{waveText(r, ui.distanceUnits)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {f.days.length > 0 && (
        <ul aria-label="Prevailing shelter by day" className="flex min-w-0 flex-col gap-1 text-xs">
          {f.days.map((d) => (
            <li key={d.date} className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <span className="shrink-0 font-medium text-foreground">{formatRowDate(d.date)}</span>
              <span className="tabular-nums text-muted-foreground">
                {d.dirPrevailing ? `${d.dirPrevailing}, ` : ''}
                {d.windKtsMin !== null && d.windKtsMax !== null ? `${kn(d.windKtsMin)}-${kn(d.windKtsMax)} kn` : '-- kn'}
                {d.gustKtsMax !== null ? `, gusts to ${kn(d.gustKtsMax)}` : ''}
              </span>
              {d.shelterPrevailing !== '' && <GradeBadge grade={d.shelterPrevailing} />}
            </li>
          ))}
        </ul>
      )}
      {f.note && <p className="text-xs text-muted-foreground">{f.note}</p>}
      {f.wavesError && <p className="text-xs text-muted-foreground">Waves unavailable: {f.wavesError}</p>}
      {f.wavesNoData && <p className="text-xs text-muted-foreground">The wave model has no figures at this point.</p>}
    </Block>
  )
}

function TideBlock({ card }: { card: AnchorageCard }) {
  const { ui } = useAppConfig()
  const t = card.tide
  return (
    <Block title="Tide" testId="anchorage-tide" error={t.error}>
      <p className="min-w-0 truncate text-xs text-muted-foreground" title={`${t.station}, ${distanceText(t.distanceNm * METRES_PER_NM, ui.distanceUnits)} away`}>
        {t.station}, {Number(t.distanceNm.toFixed(1))} nm from the anchorage
      </p>
      <p data-testid="anchorage-tide-arrival" className="text-xs tabular-nums text-foreground">
        {t.heightAtArrivalM !== null
          ? `At arrival ${lengthText(t.heightAtArrivalM, ui.distanceUnits)}${t.direction ? `, ${t.direction.toLowerCase()}` : ''}`
          : <span className="text-muted-foreground">{t.heightNote || 'Height at arrival not available'}</span>}
      </p>
      {t.extremes.length > 0 && (
        <ul aria-label="High and low waters" className="flex min-w-0 flex-col gap-0.5 text-xs tabular-nums">
          {t.extremes.map((e) => (
            <li key={e.time} className={cn('flex min-w-0 flex-wrap items-baseline gap-x-2', e.inWindow ? 'text-foreground' : 'text-muted-foreground')}>
              <span className="w-10 shrink-0">{e.type === 'high' ? 'High' : 'Low'}</span>
              <span className="whitespace-nowrap">{dateTimeText(e.time, card.timezone)}</span>
              <span>{lengthText(e.heightM, ui.distanceUnits)}</span>
              {!e.inWindow && <span className="text-muted-foreground">outside the stay</span>}
            </li>
          ))}
        </ul>
      )}
      {t.note && <p data-testid="anchorage-tide-note" className="text-xs text-muted-foreground">{t.note}</p>}
      <p data-testid="anchorage-tide-range" className="text-xs text-muted-foreground">
        {t.rangeM !== null ? `Range ${lengthText(t.rangeM, ui.distanceUnits)}. ` : ''}
        {t.rangeNote}
      </p>
      <p className="text-xs text-muted-foreground">Predicted on the tide table's datum, not charted depth. Times are local to the anchorage.</p>
    </Block>
  )
}

function FeaturesBlock({ card }: { card: AnchorageCard }) {
  const { ui } = useAppConfig()
  const f = card.features
  const groups = groupMoorings(f.moorings, ui.distanceUnits)
  const nothing = f.restrictedAreas.length + f.moorings.length + f.anchorageAreas.length + f.seabedAreas.length === 0 && f.bay === null
  return (
    <Block title="Local features" testId="anchorage-features" error={f.error}>
      {f.restrictedAreas.length > 0 && (
        <ul aria-label="Restricted areas" className="flex min-w-0 flex-col gap-1">
          {f.restrictedAreas.map((r) => {
            const line = restrictedLine(r, ui.distanceUnits)
            return (
              <li key={r.osm} className={cn('flex min-w-0 flex-col rounded border px-2 py-1 text-xs', TONE[line.tone])}>
                <span className="min-w-0 truncate font-medium">{line.label}</span>
                <span className="min-w-0">{line.detail}</span>
              </li>
            )
          })}
        </ul>
      )}
      {f.moorings.length > 0 && (
        <div className="flex min-w-0 flex-col gap-0.5 text-xs">
          <p className="font-medium text-foreground">
            {f.moorings.length} public {f.moorings.length === 1 ? 'mooring' : 'moorings'} within 1 nm
          </p>
          <ul aria-label="Moorings" className="flex min-w-0 flex-col gap-0.5">
            {groups.map((g) => (
              <li key={g.label} className={cn('flex min-w-0 flex-wrap items-baseline gap-x-2', g.untagged && 'text-muted-foreground')}>
                <span className="tabular-nums">{g.count} ×</span>
                <span className="min-w-0">{g.label}</span>
                <span className="tabular-nums text-muted-foreground">nearest {distanceText(g.nearestM, ui.distanceUnits)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {f.anchorageAreas.length > 0 && (
        <ul aria-label="Anchorage areas" className="flex min-w-0 flex-col gap-0.5 text-xs">
          {f.anchorageAreas.map((a) => (
            <li key={a.osm} className="min-w-0 truncate">
              <span className="font-medium text-foreground">Anchorage area</span>
              {a.name ? ` · ${a.name}` : ''}
              {a.category ? ` · ${humanise(a.category)}` : ''}
              <span className="text-muted-foreground"> · {distanceText(a.distanceM, ui.distanceUnits)}</span>
            </li>
          ))}
        </ul>
      )}
      {f.seabedAreas.length > 0 && (
        <ul aria-label="Seabed areas" className="flex min-w-0 flex-col gap-0.5 text-xs">
          {f.seabedAreas.map((a) => (
            <li key={a.osm} className="min-w-0 truncate">
              <span className="font-medium text-foreground">Seabed</span> · {humanise(a.surface)}
              {a.name ? ` · ${a.name}` : ''}
              <span className="text-muted-foreground"> · {distanceText(a.distanceM, ui.distanceUnits)}</span>
            </li>
          ))}
        </ul>
      )}
      {f.bay && (
        <p className="min-w-0 truncate text-xs">
          <span className="font-medium text-foreground">Bay</span> · {f.bay.name}
          <span className="text-muted-foreground"> · {f.bay.contains ? 'you are inside it' : `${distanceText(f.bay.distanceM, ui.distanceUnits)} away`}</span>
        </p>
      )}
      {nothing && <p className="text-xs text-muted-foreground">Nothing is mapped within 1 nm. That does not mean there is nothing there.</p>}
      {f.truncated && <p className="text-xs text-muted-foreground">The map has more than this card lists: lists may be incomplete.</p>}
    </Block>
  )
}

/**
 * The anchorage under a Mate reply (ADR 0244): the shelter rose, the forecast
 * and the tide for the stay, what the map records nearby and a map. The server
 * built it from what the tool read, and a reopened thread draws the same card.
 * A section that could not be checked says why and shows nothing else.
 */
export const AssistantAnchorageCard = memo(function AssistantAnchorageCard({ card }: { card: AnchorageCard }) {
  const [mapOpen, setMapOpen] = useState(false)
  const stay = `${dateTimeText(card.arrive, card.timezone)} to ${dateTimeText(card.leave, card.timezone)}`
  return (
    <section aria-label="Anchorage" data-testid="assistant-anchorage-card" className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3">
      <div className="flex min-w-0 items-center gap-2">
        <Anchor className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Anchorage</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground" title={card.name}>
          {card.name || 'Unnamed anchorage'}
        </span>
      </div>
      <p data-testid="anchorage-stay" className="min-w-0 truncate text-xs tabular-nums text-muted-foreground" title={`Local time at the anchorage (${card.timezone})`}>
        {stay}
      </p>

      <ShelterBlock card={card} />
      <ForecastBlock card={card} />
      <TideBlock card={card} />
      <FeaturesBlock card={card} />

      <details className="group min-w-0 border-t border-border pt-2" onToggle={(e) => setMapOpen(e.currentTarget.open)}>
        <summary className="cursor-pointer list-none text-xs font-medium text-foreground marker:hidden [&::-webkit-details-marker]:hidden">
          <span className="mr-1 inline-block text-muted-foreground transition-transform group-open:rotate-90" aria-hidden="true">
            ▸
          </span>
          Map
        </summary>
        <div className="pt-2">{mapOpen && <AssistantAnchorageMap card={card} />}</div>
      </details>

      <div data-testid="anchorage-footer" className="flex min-w-0 flex-col gap-0.5 border-t border-border pt-2 text-xs text-muted-foreground">
        {footerLines(card).map((line) => (
          <p key={line} className="min-w-0 truncate">
            {line}
          </p>
        ))}
      </div>
    </section>
  )
})
