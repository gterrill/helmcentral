import { useMemo } from 'react'

import { useInventoryDecks } from '@/hooks/use-inventory'
import { locationDeckLabel } from '@/lib/index-order'

/**
 * The decks and the Deck column's text for a location, in one place. The
 * Locations table, its rows and a location's arrows all use the same label, so
 * that when the decks never loaded a located zone reads `?` everywhere
 * rather than "on no deck".
 */
export function useLocationDeckLabel() {
  const decksState = useInventoryDecks()
  const unavailable = decksState.error !== null && decksState.decks.length === 0
  const deckLabel = useMemo(() => locationDeckLabel(decksState.decks, unavailable), [decksState.decks, unavailable])
  return { decksState, deckLabel }
}
