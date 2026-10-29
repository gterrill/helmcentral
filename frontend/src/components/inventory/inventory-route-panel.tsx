import { memo } from 'react'
import { toast } from 'sonner'

import { InventoryPanel } from '@/components/inventory/inventory-panel'
import { useInventoryRoute } from '@/hooks/use-inventory-route'
import { compactInventoryList, inventorySectionOf, type InventoryList, type InventoryRoute } from '@/lib/app-location'

const NO_LIST: InventoryList = {}

interface InventoryRoutePanelProps {
  canWrite: boolean
}

// ADR 0171: Inventory wired to the app location. The panels that hold unsaved
// state (the Equipment, location and deck editors, Stocktake, the bin's
// quick-add) register their own navigation guards, so nothing here mirrors
// what they hold.
// AppShell re-renders on every live delta; memo keeps this page out of it.
export const InventoryRoutePanel = memo(function InventoryRoutePanel({ canWrite }: InventoryRoutePanelProps) {
  const { route, go, close, goSection, locationsHome } = useInventoryRoute()

  const section = inventorySectionOf(route)

  // The filter, search and sort of the index the operator is on, or of the one
  // the record on screen was opened from. A record carries it so its arrows
  // step through that list and Back returns to it. Moving to a record of
  // another kind starts clean.
  const inEquipment = (route.view === 'index' && route.section === 'equipment') || route.view === 'equipment'
  const inLocations = (route.view === 'index' && route.section === 'locations') || route.view === 'location' || route.view === 'plan'
  const inDecks = route.view === 'decks' || route.view === 'deck'
  const list: InventoryList | undefined =
    (inEquipment || inLocations || inDecks) && 'list' in route ? route.list : undefined
  const carry = (applies: boolean) => (applies && list ? { list } : {})
  const creating = route.view === 'equipment-new'
  const binCode = route.view === 'bin' ? route.code : null

  // A create, delete or discard that already succeeded has nothing left to
  // protect, and the editor still holds its guard at the moment it calls
  // these (its draft is never re-baselined: it unmounts or reloads instead).
  // Going through the guard would pop "unsaved changes" straight after a
  // successful save, and on a delete offer a Save that would PUT to a record
  // the server had just dropped. So these skip the guard.
  const settle = (view: InventoryRoute) => {
    go(view, { force: true })
  }

  return (
    <InventoryPanel
      activeSectionId={section}
      onSectionChange={(id) => { goSection(id) }}
      equipmentEditId={route.view === 'equipment' ? route.id : null}
      creatingEquipment={creating}
      newEquipmentPreset={route.view === 'equipment-new' ? route.preset : null}
      onOpenEquipment={(id) => { go({ view: 'equipment', id, ...carry(inEquipment) }) }}
      onNewEquipment={(preset) => { go({ view: 'equipment-new', preset: preset ?? null }) }}
      onCloseEditor={() => { close() }}
      onEquipmentCreated={(id) => { settle({ view: 'equipment', id }) }}
      onEquipmentDeleted={(message) => {
        settle({ view: 'index', section: 'equipment', ...carry(inEquipment) })
        // The item is gone either way: the message is only ever the server's
        // own "the item was deleted, but a photo file/document could not be
        // removed" warning, shown on the destination (the Equipment index)
        // rather than keeping the editor open on a record that no longer exists.
        if (message) toast.error(message)
      }}
      onEquipmentDiscarded={() => { settle({ view: 'index', section: 'equipment' }) }}
      list={list ?? NO_LIST}
      onListChange={(next) => {
        // A search keystroke or a header click is not a place to go Back to:
        // it replaces the entry.
        const compact = compactInventoryList(next)
        const withList = compact ? { list: compact } : {}
        if (inEquipment) go({ view: 'index', section: 'equipment', ...withList }, { replace: true })
        else if (inLocations) go({ view: 'index', section: 'locations', ...withList }, { replace: true })
        else if (inDecks) go({ view: 'decks', ...withList }, { replace: true })
      }}
      canWrite={canWrite}
      binCode={binCode}
      onOpenBin={(code) => { go({ view: 'bin', code }) }}
      // Back is "up": a resolved bin returns to its own location's page,
      // whether the operator came from there or from a tag scan. No zone
      // (unknown bin, failed load) is the Locations index.
      onCloseBin={(zoneId) => { go(zoneId ? { view: 'location', id: zoneId } : locationsHome()) }}
      locationEditId={route.view === 'location' ? route.id : null}
      onOpenLocation={(id) => { go({ view: 'location', id, ...carry(inLocations) }) }}
      onCloseLocation={() => { close() }}
      onLocationDeleted={() => { settle(locationsHome(undefined, inLocations ? list : undefined)) }}
      locationsView={route.view === 'plan' ? 'plan' : 'table'}
      onLocationsViewChange={(view) => { go(locationsHome(view === 'plan', inLocations ? list : undefined)) }}
      planDeckId={route.view === 'plan' ? route.deckId : null}
      onPlanDeckChange={(deckId) => { go({ view: 'plan', deckId, ...carry(inLocations) }) }}
      decksOpen={route.view === 'decks' || route.view === 'deck'}
      deckEditId={route.view === 'deck' ? route.id : null}
      onOpenDecks={() => { go({ view: 'decks' }) }}
      onOpenDeck={(id) => { go({ view: 'deck', id, ...carry(inDecks) }) }}
      onCloseDecks={() => { go(locationsHome()) }}
      onCloseDeck={() => { go({ view: 'decks', ...carry(inDecks) }) }}
      onDeckDeleted={() => { settle({ view: 'decks', ...carry(inDecks) }) }}
    />
  )
})
