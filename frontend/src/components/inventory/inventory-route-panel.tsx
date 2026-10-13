import { toast } from 'sonner'

import { InventoryPanel } from '@/components/inventory/inventory-panel'
import { useInventoryRoute } from '@/hooks/use-inventory-route'
import { inventorySectionOf, type InventoryRoute } from '@/lib/app-location'

interface InventoryRoutePanelProps {
  canWrite: boolean
}

// ADR 0171: Inventory wired to the app location. The panels that hold unsaved
// state (the Equipment, location and deck editors, Stocktake, the bin's
// quick-add) register their own navigation guards, so nothing here mirrors
// what they hold.
export function InventoryRoutePanel({ canWrite }: InventoryRoutePanelProps) {
  const { route, go, close, goSection, locationsHome } = useInventoryRoute()

  const section = inventorySectionOf(route)
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
      onOpenEquipment={(id) => { go({ view: 'equipment', id }) }}
      onNewEquipment={(preset) => { go({ view: 'equipment-new', preset: preset ?? null }) }}
      onCloseEditor={() => { close() }}
      onEquipmentCreated={(id) => { settle({ view: 'equipment', id }) }}
      onEquipmentDeleted={(message) => {
        settle({ view: 'index', section: 'equipment' })
        // The item is gone either way: the message is only ever the server's
        // own "the item was deleted, but a photo file/document could not be
        // removed" warning, shown on the destination (the Equipment index)
        // rather than keeping the editor open on a record that no longer exists.
        if (message) toast.error(message)
      }}
      onEquipmentDiscarded={() => { settle({ view: 'index', section: 'equipment' }) }}
      canWrite={canWrite}
      binCode={binCode}
      onOpenBin={(code) => { go({ view: 'bin', code }) }}
      // Back is "up": a resolved bin returns to its own location's page,
      // whether the operator came from there or from a tag scan. No zone
      // (unknown bin, failed load) is the Locations index.
      onCloseBin={(zoneId) => { go(zoneId ? { view: 'location', id: zoneId } : locationsHome()) }}
      locationEditId={route.view === 'location' ? route.id : null}
      onOpenLocation={(id) => { go({ view: 'location', id }) }}
      onCloseLocation={() => { close() }}
      onLocationDeleted={() => { settle(locationsHome()) }}
      locationsView={route.view === 'plan' ? 'plan' : 'table'}
      onLocationsViewChange={(view) => { go(locationsHome(view === 'plan')) }}
      planDeckId={route.view === 'plan' ? route.deckId : null}
      onPlanDeckChange={(deckId) => { go({ view: 'plan', deckId }) }}
      decksOpen={route.view === 'decks' || route.view === 'deck'}
      deckEditId={route.view === 'deck' ? route.id : null}
      onOpenDecks={() => { go({ view: 'decks' }) }}
      onOpenDeck={(id) => { go({ view: 'deck', id }) }}
      onCloseDecks={() => { go(locationsHome()) }}
      onCloseDeck={() => { go({ view: 'decks' }) }}
      onDeckDeleted={() => { settle({ view: 'decks' }) }}
    />
  )
}
