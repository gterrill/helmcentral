import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { decodeMateCards, type MarinaCard } from '@/lib/mate-cards'

/**
 * The stored card the Go side writes for Coral Sea Marina
 * (backend/testdata/mate_cards/marina_coral_sea.json, rewritten by
 * `go test -run MarinaCardGolden -update`). The tests decode the real bytes so
 * the two sides cannot drift apart.
 */
export function marinaPayload(over: Record<string, unknown> = {}): Record<string, unknown> {
  const stored = JSON.parse(readFileSync(join(__dirname, '../../../../backend/testdata/mate_cards/marina_coral_sea.json'), 'utf8'))
  return { ...stored.payload, ...over }
}

export function marinaCard(over: Record<string, unknown> = {}): MarinaCard {
  const [card] = decodeMateCards([{ kind: 'marina', payload: marinaPayload(over) }])
  if (card.kind !== 'marina') throw new Error(`fixture did not decode: ${JSON.stringify(card)}`)
  return card
}

/** A card with nothing drawn: no outline and no pontoons. */
export function marinaCardWithoutGeometry(over: Record<string, unknown> = {}): MarinaCard {
  const geometry = { ...(marinaPayload().geometry as object), outline: [], pontoons: [] }
  return marinaCard({ geometry, ...over })
}
