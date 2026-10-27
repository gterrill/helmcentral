import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { shownAge } from '@/lib/staleness'
import { createTelemetryStore, keepUnchanged, keepUnchangedItems, type SubscribeTelemetry, type TelemetryUpdate } from '@/lib/telemetry-store'

interface SolarControllerState {
  id: string
  label: string
  current_w: number
  today_kwh: number
  yesterday_kwh: number
  mode: string
  error: string
  last_update_age_s: number
  contribution_pct: number
}

interface SolarState {
  datetime: string
  source: string
  current_w: number
  today_kwh: number
  yesterday_kwh: number
  peak_today_w: number
  last_update_age_s: number
  controllers: SolarControllerState[]
}

export interface SolarController {
  id: string
  label: string
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  mode: string | null
  error: string | null
  lastUpdateAgeS: number | null
  contributionPct: number | null
}

function normalizeNumber(value: unknown): number | null {
  return typeof value === 'number' && value >= 0 ? value : null
}

function normalizeText(value: unknown): string | null {
  return typeof value === 'string' && value.trim().length > 0 ? value.trim() : null
}

/** The solar state as the Solar tile reads it. */
export interface SolarStateValue {
  currentW: number | null
  todayKWh: number | null
  yesterdayKWh: number | null
  peakTodayW: number | null
  lastUpdateAgeS: number | null
  /** Null until the first event: an empty list is a server with no controllers, not one not heard from yet. */
  controllers: SolarController[] | null
}

/** Every reading unknown: the state before the first solar-state event. */
export const INITIAL_SOLAR_STATE: SolarStateValue = {
  currentW: null,
  todayKWh: null,
  yesterdayKWh: null,
  peakTodayW: null,
  lastUpdateAgeS: null,
  controllers: null,
}

function parseController(controller: SolarControllerState): SolarController {
  return {
    id: normalizeText(controller.id) ?? 'unknown',
    label: normalizeText(controller.label) ?? 'Array',
    currentW: normalizeNumber(controller.current_w),
    todayKWh: normalizeNumber(controller.today_kwh),
    yesterdayKWh: normalizeNumber(controller.yesterday_kwh),
    mode: normalizeText(controller.mode),
    error: normalizeText(controller.error),
    lastUpdateAgeS: normalizeNumber(controller.last_update_age_s),
    contributionPct: normalizeNumber(controller.contribution_pct),
  }
}

function sameController(a: SolarController, b: SolarController): boolean {
  return a.id === b.id && a.label === b.label && a.currentW === b.currentW && a.todayKWh === b.todayKWh
    && a.yesterdayKWh === b.yesterdayKWh && a.mode === b.mode && a.error === b.error
    && a.contributionPct === b.contributionPct && shownAge(a.lastUpdateAgeS) === shownAge(b.lastUpdateAgeS)
}

/*
 * One store for the `solar-state` event (ADR 0215; lib/telemetry-store.ts).
 * trend_24h_total is not parsed: nothing shows it. A controller that shows the
 * same keeps its object, ages compared on what the tile shows for them, and a
 * tick that changes nothing the tile shows keeps the snapshot.
 */
function applySolarState(previous: SolarStateValue, raw: string): TelemetryUpdate<SolarStateValue> | null {
  let data: SolarState
  try {
    data = JSON.parse(raw) as SolarState
  } catch (err) {
    console.error('Failed to parse solar state event:', err)
    return null
  }
  const incoming = Array.isArray(data.controllers) ? data.controllers.map(parseController) : []
  const parsed: SolarStateValue = {
    currentW: normalizeNumber(data.current_w),
    todayKWh: normalizeNumber(data.today_kwh),
    yesterdayKWh: normalizeNumber(data.yesterday_kwh),
    peakTodayW: normalizeNumber(data.peak_today_w),
    lastUpdateAgeS: normalizeNumber(data.last_update_age_s),
    // Keyed by id and place in the list: a controller with no id parses to
    // 'unknown', and several of those must each meet their own previous row.
    controllers: keepUnchangedItems(previous.controllers, incoming, (c, index) => `${c.id}#${index}`, sameController),
  }
  const next = keepUnchanged(previous, parsed, (field, a, b) => (
    field === 'lastUpdateAgeS' ? shownAge(a as number | null) === shownAge(b as number | null) : Object.is(a, b)
  ))
  return next === null ? null : { state: next }
}

// Read at start, not at import: a module that only imports this one's
// helpers or types never touches the stream.
const listenToStream: SubscribeTelemetry = (event, onData) => subscribeTelemetry(event, onData)

const store = createTelemetryStore(listenToStream, 'solar-state', INITIAL_SOLAR_STATE, applySolarState)

const selectAll = (state: SolarStateValue) => state

/** The whole solar state: the Solar tile shows all of it. */
export function useSolarState(): SolarStateValue {
  return store.useSelector(selectAll)
}
