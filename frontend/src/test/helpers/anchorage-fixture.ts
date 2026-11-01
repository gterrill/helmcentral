import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { decodeMateCards, type AnchorageCard } from '@/lib/mate-cards'

/**
 * The stored card the Go side writes for West Butterfly Bay
 * (backend/testdata/mate_cards/anchorage_butterfly_bay.json, rewritten by
 * `go test -run AnchorageCardGolden -update`). The tests decode the real bytes
 * so the two sides cannot drift apart.
 */
export function anchoragePayload(over: Record<string, unknown> = {}): Record<string, unknown> {
  const stored = JSON.parse(readFileSync(join(__dirname, '../../../../backend/testdata/mate_cards/anchorage_butterfly_bay.json'), 'utf8'))
  return { ...stored.payload, ...over }
}

export function anchorageCard(over: Record<string, unknown> = {}): AnchorageCard {
  const [card] = decodeMateCards([{ kind: 'anchorage', payload: anchoragePayload(over) }])
  if (card.kind !== 'anchorage') throw new Error(`fixture did not decode: ${JSON.stringify(card)}`)
  return card
}

const base = anchoragePayload()

/** A card whose forecast, tide and features all failed. */
export function anchorageCardWithErrors(): AnchorageCard {
  return anchorageCard({
    forecast: { ...(base.forecast as object), error: 'Forecast unavailable: weather provider "open-meteo": timed out', rows: [], days: [] },
    tide: { ...(base.tide as object), error: 'Tide unavailable: tide provider "bom" has no stations', extremes: [], height_at_arrival_m: null, range_m: null },
    features: { ...(base.features as object), error: 'Features unavailable: plugin "google-places" does not provide anchorage features', restricted_areas: [], moorings: [], anchorage_areas: [], seabed_areas: [], bay: null },
  })
}

const SKIPPED = { status: 'skipped', skipped_reason: 'the depth is unknown' }

/** The depth section Go writes when no depth is known: every check skipped with its reason. */
export function unknownDepth(): Record<string, unknown> {
  const d = base.depth as Record<string, unknown> // the golden always carries a depth
  return {
    ...d,
    known: false,
    depth_m: null,
    source: 'none',
    saved_place: '',
    saved_source: '',
    saved_date: '',
    note: 'The depth is unknown: check the chart.',
    clearance: { ...(d.clearance as object), ...SKIPPED, lowest_water_at: '', lowest_water_height_m: null, depth_at_low_m: null, draft_m: null, clearance_m: null, wanted_m: null },
    rode: { ...(d.rode as object), ...SKIPPED, highest_water_at: '', highest_water_height_m: null, depth_at_high_m: null, method: '', wind_kts: null, rode_needed_m: null, scope_ratio: null, chain_onboard_m: null, short_by_m: null, wind_checked_until: '' },
    swing: { ...(d.swing as object), ...SKIPPED, radius_m: null, rode_m: null, bow_offset_m: null, loa_m: null, all_chain_out: false },
  }
}

/** A card for an anchorage with no depth known. */
export function anchorageCardWithoutDepth(): AnchorageCard {
  return anchorageCard({ depth: unknownDepth() })
}
