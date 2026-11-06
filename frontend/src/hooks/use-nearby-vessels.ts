import { useMemo } from 'react'

import { aisSilenceKey } from '@/lib/nearby-vessel-display'
import { ageFromPayload, shownAge } from '@/lib/staleness'

import { createTelemetryStore, keepUnchangedItems, sameJSON, sameShallow, type TelemetryUpdate } from '@/lib/telemetry-store'

export type NearbyVessel = {
  id: string
  name: string
  mmsi?: string
  range_m: number
  age_seconds: number
  sog_knots?: number
  lat?: number
  lon?: number
  seen_count?: number
  last_seen_at?: string
}

type NearbyVesselsResponse = {
  vessels?: NearbyVessel[]
  max_range_m?: unknown
  source?: unknown
  ais_heard_age_s?: unknown
  capped_count?: unknown
}

/** What the nearby-vessels store holds. */
export interface NearbyVesselsValue {
  vessels: NearbyVessel[]
  // Age of the freshest contact (ADR 0068), distinct from each vessel's own
  // age_seconds. Null with no contacts: an empty horizon is not a dead feed.
  lastUpdateAgeS: number | null
  // How far the backend searches, in metres. Null until a payload carries it;
  // the empty state then names no distance rather than inventing one.
  maxRangeM: number | null
  // Where the backend got the list: "signalk" when it read the live AIS feed,
  // anything else (it sends "backend-fallback") when it could not. Null until a
  // payload carries it.
  source: string | null
  // Seconds since any other vessel last reported a position, in range or not.
  // Null until a payload carries it; -1 when the backend holds no report. Kept
  // at the first value that reads the same on screen, so the map's reader is
  // not woken every five seconds while AIS is flowing.
  aisHeardAgeS: number | null
  // Boats near the anchor the backend left out during a watch because the feed
  // is capped. Zero when nothing was left out.
  cappedCount: number
}

const INITIAL: NearbyVesselsValue = { vessels: [], lastUpdateAgeS: null, maxRangeM: null, source: null, aisHeardAgeS: null, cappedCount: 0 }

function isVessel(item: NearbyVessel): boolean {
  return typeof item.id === 'string' &&
    item.id !== '' &&
    typeof item.name === 'string' &&
    typeof item.range_m === 'number' &&
    Number.isFinite(item.range_m) &&
    typeof item.age_seconds === 'number' &&
    Number.isFinite(item.age_seconds)
}

function toVessel(item: NearbyVessel): NearbyVessel {
  return {
    ...item,
    mmsi: typeof item.mmsi === 'string' && item.mmsi.trim() !== '' ? item.mmsi : undefined,
    lat: typeof item.lat === 'number' && Number.isFinite(item.lat) ? item.lat : undefined,
    lon: typeof item.lon === 'number' && Number.isFinite(item.lon) ? item.lon : undefined,
    seen_count: typeof item.seen_count === 'number' && Number.isFinite(item.seen_count) ? Math.max(0, Math.floor(item.seen_count)) : 0,
    last_seen_at: typeof item.last_seen_at === 'string' ? item.last_seen_at : undefined,
  }
}

/*
 * One store for the `nearby-vessels` event (ADR 0215; lib/telemetry-store.ts).
 * Every field of a vessel counts, age_seconds included: the list shows it on
 * every row ("2m ago"), so on the boat, where every contact's age moves on
 * every five-second tick, the list and the maps redraw with each one. The
 * feed's own age counts only when what a tile shows for it changes.
 */
function applyNearbyVessels(previous: NearbyVesselsValue, payload: unknown): TelemetryUpdate<NearbyVesselsValue> | null {
  const data = payload as NearbyVesselsResponse & { last_update_age_s?: unknown }
  const list = Array.isArray(data.vessels) ? data.vessels : []
  const vessels = keepUnchangedItems(previous.vessels, list.filter(isVessel).map(toVessel), (v) => v.id, sameJSON)
  const maxRangeM = typeof data.max_range_m === 'number' && Number.isFinite(data.max_range_m) && data.max_range_m > 0 ? data.max_range_m : null
  const source = typeof data.source === 'string' ? data.source : null
  const heard = data.ais_heard_age_s
  const heardAge = typeof heard === 'number' && Number.isFinite(heard) ? heard : null
  const aisHeardAgeS = aisSilenceKey(heardAge) === aisSilenceKey(previous.aisHeardAgeS) ? previous.aisHeardAgeS : heardAge
  const cappedCount = typeof data.capped_count === 'number' && Number.isFinite(data.capped_count) && data.capped_count > 0 ? Math.floor(data.capped_count) : 0
  const age = ageFromPayload(data.last_update_age_s)
  const lastUpdateAgeS = shownAge(age) === shownAge(previous.lastUpdateAgeS) ? previous.lastUpdateAgeS : age
  if (vessels === previous.vessels && maxRangeM === previous.maxRangeM && source === previous.source && aisHeardAgeS === previous.aisHeardAgeS && cappedCount === previous.cappedCount && lastUpdateAgeS === previous.lastUpdateAgeS) return null
  return { state: { vessels, lastUpdateAgeS, maxRangeM, source, aisHeardAgeS, cappedCount } }
}

const store = createTelemetryStore('nearby-vessels', INITIAL, applyNearbyVessels)

const selectAll = (state: NearbyVesselsValue) => state
const selectFeed = (state: NearbyVesselsValue) => ({ source: state.source, maxRangeM: state.maxRangeM, aisHeardAgeS: state.aisHeardAgeS })
const selectVessels = (state: NearbyVesselsValue) => state.vessels

/** The Nearby Vessels list: the vessels, `loading` until the first event, the feed age and the search range. */
export function useNearbyVessels() {
  const heard = store.useHeardSelector(selectAll)
  return useMemo(() => ({
    vessels: (heard ?? INITIAL).vessels,
    loading: heard === null,
    lastUpdateAgeS: (heard ?? INITIAL).lastUpdateAgeS,
    maxRangeM: (heard ?? INITIAL).maxRangeM,
    source: (heard ?? INITIAL).source,
    aisHeardAgeS: (heard ?? INITIAL).aisHeardAgeS,
    cappedCount: (heard ?? INITIAL).cappedCount,
  }), [heard])
}

/** The vessels alone, for the maps' AIS markers: a change to the feed age leaves them alone. */
export function useNearbyVesselList(): NearbyVessel[] {
  return store.useSelector(selectVessels)
}

export type NearbyVesselFeed = Pick<NearbyVesselsValue, 'source' | 'maxRangeM'> & Partial<Pick<NearbyVesselsValue, 'aisHeardAgeS'>>

/**
 * What the Nearby map needs to word an empty Vessels group: the feed's source
 * and search range. Null until the first event. Compared by value, so the
 * five-second age ticks and feed-age changes never re-render the reader.
 */
export function useNearbyVesselFeed(): NearbyVesselFeed | null {
  return store.useHeardSelector(selectFeed, sameShallow)
}
