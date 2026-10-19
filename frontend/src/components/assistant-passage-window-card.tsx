import { CalendarClock } from 'lucide-react'
import { useState } from 'react'

import type { PassageMeasure, PassageState, PassageWindowCard, PassageWindowCell } from '@/lib/mate-cards'

const STALE_AFTER_MS = 6 * 60 * 60 * 1000

const STATE_WORD: Record<PassageState, string> = {
  go: 'GO',
  marginal: 'MARGINAL',
  no_go: 'NO-GO',
  no_data: 'NO DATA',
}

// Alert semantics only: the same palette as tempClass() and friends. The word
// on the chip carries the state; the colour just helps at a glance.
const STATE_CHIP: Record<PassageState, string> = {
  go: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300',
  marginal: 'bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300',
  no_go: 'bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300',
  no_data: 'bg-muted text-muted-foreground',
}

const MEASURE_WORDS: Record<PassageMeasure, string> = {
  // Only on cards saved by v0.49.0 chats; marginal is worked out now and never listed.
  wind_marginal: 'marginal wind limit',
  gust: 'gusts',
  wave: 'wave height',
  wind_against_tide: 'wind against tide',
  daylight: 'daylight arrival',
}
const MEASURE_ORDER: PassageMeasure[] = ['wind_marginal', 'gust', 'wave', 'wind_against_tide', 'daylight']

function StateChip({ state }: { state: PassageState }) {
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-xs font-semibold leading-none ${STATE_CHIP[state]}`}
    >
      {STATE_WORD[state]}
    </span>
  )
}

function formatTime(iso: string, timeZone: string): string {
  return new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(new Date(iso))
}

function formatStamp(iso: string, timeZone: string): string {
  const d = new Date(iso)
  const day = new Intl.DateTimeFormat('en-GB', { timeZone, weekday: 'short' }).format(d)
  return `${day} ${formatTime(iso, timeZone)}`
}

/** "Thu 23 Oct" from a local YYYY-MM-DD; read as UTC so no zone shifts the day. */
function formatDate(ymd: string): string {
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', weekday: 'short', day: 'numeric', month: 'short' }).formatToParts(
    new Date(`${ymd}T00:00:00Z`),
  )
  const get = (t: string) => parts.find((p) => p.type === t)?.value ?? ''
  return `${get('weekday')} ${get('day')} ${get('month')}`
}

function stayLabel(n: number | null): string | null {
  if (n === null) return null
  return n === 1 ? '1 night' : `${n} nights`
}

function localDay(iso: string, timeZone: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(iso))
}

/**
 * "06:00 → 14:30", with the arrival weekday appended when it lands on a later
 * local day and the departure weekday prefixed when the leg sails on another
 * day than its row's departure date ("Thu 06:00 → 11:34").
 */
function formatSpan(cell: PassageWindowCell, rowDate: string, timeZone: string): string {
  const lead = cell.date !== rowDate ? `${new Intl.DateTimeFormat('en-GB', { timeZone, weekday: 'short' }).format(new Date(cell.depart))} ` : ''
  const span = `${lead}${formatTime(cell.depart, timeZone)} → ${formatTime(cell.arrive, timeZone)}`
  if (localDay(cell.arrive, timeZone) <= localDay(cell.depart, timeZone)) return span
  return `${span} ${new Intl.DateTimeFormat('en-GB', { timeZone, weekday: 'short' }).format(new Date(cell.arrive))}`
}

function cellTitle(cell: PassageWindowCell, rowDate: string, timeZone: string): string {
  const times = formatSpan(cell, rowDate, timeZone)
  const text = cell.limiting?.text ?? cell.note
  return text ? `${text} (${times})` : times
}

/**
 * The passage-window grid under a Mate reply (ADR 0191): departure dates down,
 * legs across, one word per cell. From sm up it is a real grid; on a phone each
 * departure date is a block with its legs listed under it.
 */
export function AssistantPassageWindowCard({ card }: { card: PassageWindowCard }) {
  const [now] = useState(() => Date.now())
  const stale = now - Date.parse(card.forecastFetchedAt) > STALE_AFTER_MS
  const n = card.legs.length
  const gridCols = 'sm:grid-cols-[7.5rem_repeat(var(--legs),minmax(0,1fr))]'

  const notChecked = new Set<PassageMeasure>()
  for (const row of card.rows) for (const cell of row.cells) for (const m of cell.notChecked) notChecked.add(m)
  const notCheckedWords = MEASURE_ORDER.filter((m) => notChecked.has(m)).map((m) => MEASURE_WORDS[m])

  return (
    <section
      aria-label="Passage window"
      data-testid="assistant-passage-window-card"
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <CalendarClock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Passage window</span>
        <span className="min-w-0 truncate text-xs text-muted-foreground">
          Forecast as of {formatStamp(card.forecastFetchedAt, card.timezone)}
        </span>
        {stale && (
          <span className="shrink-0 rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium leading-none text-amber-800 dark:bg-amber-950 dark:text-amber-300">
            Forecast may have changed
          </span>
        )}
      </div>

      <div role="table" aria-label="Departure dates by leg" className="flex min-w-0 flex-col gap-2" style={{ '--legs': n } as React.CSSProperties}>
        <div role="row" className={`hidden min-w-0 gap-2 sm:grid ${gridCols}`}>
          <span role="columnheader" className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            Depart
          </span>
          {card.legs.map((leg, i) => (
            <div key={i} role="columnheader" className="flex min-w-0 flex-col">
              <span className="min-w-0 truncate text-xs font-medium text-foreground" title={leg.name}>
                {leg.name}
              </span>
              {stayLabel(leg.stayNightsAfter) && (
                <span className="text-xs text-muted-foreground">{stayLabel(leg.stayNightsAfter)}</span>
              )}
            </div>
          ))}
        </div>

        {card.rows.map((row) => (
          <div
            key={row.departDate}
            role="row"
            className={`grid min-w-0 grid-cols-1 gap-2 border-t border-border pt-2 ${gridCols}`}
          >
            <div role="rowheader" className="flex min-w-0 items-center justify-between gap-2 sm:flex-col sm:items-start sm:justify-start">
              <span className="min-w-0 truncate text-sm font-medium text-foreground">{formatDate(row.departDate)}</span>
              <StateChip state={row.verdict} />
            </div>
            {row.cells.map((cell) => {
              const detail = cell.limiting?.text ?? cell.note
              const leg = card.legs[cell.leg]
              return (
                <div
                  key={cell.leg}
                  role="cell"
                  title={cellTitle(cell, row.departDate, card.timezone)}
                  className="flex min-w-0 flex-col items-start gap-1"
                >
                  <span className="min-w-0 max-w-full truncate text-xs text-muted-foreground sm:hidden">{leg.name}
                    {stayLabel(leg.stayNightsAfter) && ` · ${stayLabel(leg.stayNightsAfter)}`}
                  </span>
                  <StateChip state={cell.state} />
                  <span className="min-w-0 max-w-full truncate text-xs tabular-nums text-muted-foreground">
                    {formatSpan(cell, row.departDate, card.timezone)}
                  </span>
                  {detail && <span className="line-clamp-2 min-w-0 text-xs text-muted-foreground">{detail}</span>}
                </div>
              )
            })}
          </div>
        ))}
      </div>

      {notCheckedWords.length > 0 && (
        <p className="min-w-0 text-xs text-muted-foreground">Not checked: {notCheckedWords.join(', ')}</p>
      )}
    </section>
  )
}
