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
