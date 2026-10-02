import { forwardRef, useImperativeHandle, useRef } from 'react'

import { BinPage } from '@/components/inventory/bin-page'
import { DeckEditor, type DeckEditorHandle } from '@/components/inventory/deck-editor'
import { DecksIndex } from '@/components/inventory/decks-index'
import { EquipmentEditor, type EquipmentEditorHandle } from '@/components/inventory/equipment-editor'
import { EquipmentIndex } from '@/components/inventory/equipment-index'
import { InventoryNav, type InventorySectionId } from '@/components/inventory/inventory-nav'
import { LocationEditor, type LocationEditorHandle } from '@/components/inventory/location-editor'
import { LocationsIndex, type LocationsView } from '@/components/inventory/locations-index'
import { MaintenanceSection } from '@/components/inventory/maintenance-section'
import { ProfilesSection } from '@/components/inventory/profiles-section'
import { StocktakeSection } from '@/components/inventory/stocktake-section'

// ADR 0123: InventoryNav plus whichever section is active - the Settings
// page shape (settings-page.tsx) exactly: App owns the active section (and,
// here, the Equipment index/editor split) and mirrors it to the URL (ADR
// 0074); this component is a thin composition shell with no fetches or
// navigation state of its own.
//
// No Help button of its own (removed - it used to duplicate the header's `?`,
// and worse, App.tsx's own `?` handler didn't pass inventorySection at all,
// so it always opened Equipment's help page regardless of which section was
// actually open, which is why this one existed in the first place. The
// header ? now reads inventorySection and follows the active section -
// see lib/help-links.ts's helpTargetFor and ADR 0142's CRUD Pattern Library
// note in AGENTS.md).
//
// Equipment's index and its editor are the SAME section, not two nav
// entries - which one shows is `equipmentEditId`/`creatingEquipment`, the
// same "one panel, index or editor" shape ADR 0112's wall-displays and ADR
// 0115's Documents Details page already use elsewhere in this app. Both
// pieces of state are owned by App.tsx (not local to this component) so its
// dirty-navigation guard can clear inventoryDirty the moment either one
// changes the editor is no longer showing - see App.tsx's own clearing
// effect, mirroring documentDetailsDirty's.

// Each way into and out of the Equipment editor is its own callback, named
// for what the operator did rather than for the state it happens to change.
// The first cut of this component instead exposed two setters
// (onEquipmentEditIdChange/onCreatingEquipmentChange) and called both, back
// to back, for every exit - which broke the dirty guard. App.tsx guards a
// close by stashing the navigation until the unsaved-changes dialog is
// answered, and it can stash exactly one: the second call overwrote the
// first, so Back on a dirty editor stashed "stop creating" over "close the
// editor" and Discard then ran a no-op with the editor still on screen.
//
// Splitting them also puts the question that actually matters - is this exit
// discarding the operator's work? - where it can be answered. Back is; a
// completed create or delete is not, and routing those through the guard
// popped the dialog after a save had already succeeded.
interface InventoryPanelProps {
  activeSectionId: InventorySectionId
  onSectionChange: (id: InventorySectionId) => void
  equipmentEditId: string | null
  creatingEquipment: boolean
  /** Index row click. Opening an item never discards anything. */
  onOpenEquipment: (id: string) => void
  /** Index "New item", or the bin page's "Full item" (ADR 0127) - the
   * latter passes the bin/zone to pre-set the draft with. Undefined/omitted
   * is the ordinary "New item" button: a blank draft, nothing pre-set. */
  onNewEquipment: (preset?: { zoneId?: string; binId?: string }) => void
  /** The editor's Back - the one exit that can discard an unsaved draft. */
  onCloseEditor: () => void
  /** A create that already succeeded; carries the id the server assigned. */
  onEquipmentCreated: (id: string) => void
  /** A delete that already succeeded. `message`, when given (2026-09-25
   * amendment), is EquipmentEditor's own "the item was deleted, but ..."
   * warning about a photo file/document that could not be removed
   * afterward - the item is gone either way. */
  onEquipmentDeleted: (message?: string) => void
  /** Discard on a new item: leave the editor with no guard, like a create or
   * delete that already succeeded. */
  onEquipmentDiscarded: () => void
  onDirtyChange?: (dirty: boolean) => void
  /** Release-fixes code-review finding: forwarded to Stocktake's own
   * onHasWorkChange and the bin page's quick-add onHasWorkChange - never
   * both mounted at once, so one prop covers either. App.tsx routes Open/
   * Full item through the same unsaved-work guard when this is true.
   * `detail`, when given, is wording for what would actually be lost -
   * BinQuickAdd's own doc comment on its identical prop explains when it
   * passes one. */
  onHasWorkChange?: (hasWork: boolean, detail?: string) => void
  canWrite?: boolean
  /** ADR 0127: the Locations section's bin page - `/inventory/bins/<code>`.
   * null is the ordinary Locations index; a non-null code shows that bin's
   * contents instead, the same index/page split Equipment already has for
   * equipmentEditId. */
  binCode: string | null
  /** Locations-section bin click, or a tag/deep link landing on one. */
  onOpenBin: (code: string) => void
  /** The bin page's own Back, which goes up to the bin's location page when
   * the bin resolved (`zoneId`), else to the Locations index. Release-fixes code-review finding: this used
   * to be a plain setter on the theory that the bin page holds no draft to
   * discard - true of the equipment editor, but the quick-add form living on
   * this same page (ADR 0127) can hold a staged name/photos or a photo still
   * queued for Retry, so App.tsx routes this through the same
   * requestWithinInventory guard onOpenBin/onSectionChange already use. */
  onCloseBin: (zoneId?: string) => void
  /** `/inventory/locations/<id>` - one location's own page. null is the
   * Locations index. Same index/page split as binCode and equipmentEditId. */
  locationEditId: string | null
  /** Locations index row click. Opening a location never discards anything. */
  onOpenLocation: (id: string) => void
  /** The location page's Back - the one exit that can discard an unsaved rename. */
  onCloseLocation: () => void
  /** A delete that already succeeded. */
  onLocationDeleted: () => void
  /** ADR 0156: the Locations index's Table or Plan view, the deck tab open in
   * Plan, and their setters. App.tsx keeps them in the URL. */
  locationsView: LocationsView
  onLocationsViewChange: (view: LocationsView) => void
  planDeckId: string | null
  onPlanDeckChange: (deckId: string) => void
  /** ADR 0156: the Decks list (`/inventory/decks`) and, within it, one deck's
   * page. deckEditId is only meaningful while decksOpen. */
  decksOpen: boolean
  deckEditId: string | null
  onOpenDecks: () => void
  onOpenDeck: (id: string) => void
  /** The Decks list's Back, up to the Locations index. */
  onCloseDecks: () => void
  /** The deck page's Back, up to the Decks list - the one exit that can
   * discard an unsaved layout draft. */
  onCloseDeck: () => void
  /** A delete that already succeeded. */
  onDeckDeleted: () => void
  /** The zone/bin App.tsx stashed from the last onNewEquipment(preset) call
   * - read once by EquipmentEditor when it mounts a brand new draft. null
   * for the ordinary "New item" button. */
  newEquipmentPreset: { zoneId?: string; binId?: string } | null
}

export interface InventoryPanelHandle {
  save: () => Promise<void>
}

export const InventoryPanel = forwardRef<InventoryPanelHandle, InventoryPanelProps>(function InventoryPanel(
  {
    activeSectionId,
    onSectionChange,
    equipmentEditId,
    creatingEquipment,
    onOpenEquipment,
    onNewEquipment,
    onCloseEditor,
    onEquipmentCreated,
    onEquipmentDeleted,
    onEquipmentDiscarded,
    onDirtyChange,
    onHasWorkChange,
    canWrite = true,
    binCode,
    onOpenBin,
    onCloseBin,
    locationEditId,
    onOpenLocation,
    onCloseLocation,
    onLocationDeleted,
    locationsView,
    onLocationsViewChange,
    planDeckId,
    onPlanDeckChange,
    decksOpen,
    deckEditId,
    onOpenDecks,
    onOpenDeck,
    onCloseDecks,
    onCloseDeck,
    onDeckDeleted,
    newEquipmentPreset,
  },
  ref,
) {
  // Only ever mounted while the Equipment editor itself is - save() is a
  // no-op (App.tsx's guard only ever calls it while inventoryDirty is true,
  // which itself can only be true while the editor is mounted and reporting
  // it) when nothing is bound, same contract as SettingsPageHandle/
  // DocumentDetailsPageHandle's own imperative save.
  const editorRef = useRef<EquipmentEditorHandle>(null)
  // The Locations page's rename draft is the only other thing that can be
  // dirty, and never at the same time as the Equipment editor.
  const locationEditorRef = useRef<LocationEditorHandle>(null)
  // So can the deck page's name and layout draft (ADR 0156).
  const deckEditorRef = useRef<DeckEditorHandle>(null)
  useImperativeHandle(ref, () => ({
    save: async () => {
      await editorRef.current?.save()
      await locationEditorRef.current?.save()
      await deckEditorRef.current?.save()
    },
  }), [])

  const showEquipmentEditor = activeSectionId === 'equipment' && (equipmentEditId !== null || creatingEquipment)

  const activeSection = (() => {
    if (activeSectionId === 'equipment') {
      if (showEquipmentEditor) {
        return (
          <EquipmentEditor
            ref={editorRef}
            id={equipmentEditId}
            onBack={onCloseEditor}
            onCreated={onEquipmentCreated}
            onDeleted={onEquipmentDeleted}
            onDiscarded={onEquipmentDiscarded}
            onDirtyChange={onDirtyChange}
            onHasWorkChange={onHasWorkChange}
            canWrite={canWrite}
            initialZoneId={equipmentEditId === null ? (newEquipmentPreset?.zoneId ?? null) : null}
            initialBinId={equipmentEditId === null ? (newEquipmentPreset?.binId ?? null) : null}
          />
        )
      }
      return (
        <EquipmentIndex
          onOpenItem={onOpenEquipment}
          onNewItem={onNewEquipment}
          canWrite={canWrite}
        />
      )
    }
    if (activeSectionId === 'maintenance') {
      return <MaintenanceSection onOpenEquipment={onOpenEquipment} canWrite={canWrite} />
    }
    if (activeSectionId === 'profiles') return <ProfilesSection canWrite={canWrite} />
    if (activeSectionId === 'locations') {
      // ADR 0127: the bin page is the SAME section as the Locations index,
      // not a nav entry of its own - identical "index or page" split to
      // Equipment just above (showEquipmentEditor).
      if (binCode !== null) {
        return (
          <BinPage
            code={binCode}
            onClose={onCloseBin}
            onOpenEquipment={onOpenEquipment}
            onNewEquipment={onNewEquipment}
            canWrite={canWrite}
            onHasWorkChange={onHasWorkChange}
          />
        )
      }
      if (locationEditId !== null) {
        return (
          <LocationEditor
            ref={locationEditorRef}
            id={locationEditId}
            onBack={onCloseLocation}
            onDeleted={onLocationDeleted}
            onOpenBin={onOpenBin}
            onOpenDeck={onOpenDeck}
            onOpenDecks={onOpenDecks}
            onDirtyChange={onDirtyChange}
            canWrite={canWrite}
          />
        )
      }
      if (decksOpen) {
        if (deckEditId !== null) {
          return (
            <DeckEditor
              ref={deckEditorRef}
              id={deckEditId}
              onBack={onCloseDeck}
              onDeleted={onDeckDeleted}
              onDirtyChange={onDirtyChange}
              canWrite={canWrite}
            />
          )
        }
        return <DecksIndex onOpenDeck={onOpenDeck} onBack={onCloseDecks} canWrite={canWrite} />
      }
      return (
        <LocationsIndex
          onOpenLocation={onOpenLocation}
          onOpenBin={onOpenBin}
          view={locationsView}
          onViewChange={onLocationsViewChange}
          planDeckId={planDeckId}
          onPlanDeckChange={onPlanDeckChange}
          onOpenDecks={onOpenDecks}
          canWrite={canWrite}
        />
      )
    }
    if (activeSectionId === 'stocktake') {
      return <StocktakeSection onOpenEquipment={onOpenEquipment} canWrite={canWrite} onHasWorkChange={onHasWorkChange} />
    }
    return null
  })()

  return (
    <div className="flex flex-col gap-4 md:flex-row">
      <InventoryNav activeSectionId={activeSectionId} onSelect={onSectionChange} />

      <div className="min-w-0 flex-1 space-y-4">
        {activeSection}
      </div>
    </div>
  )
})
