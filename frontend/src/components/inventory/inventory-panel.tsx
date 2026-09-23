import { BookOpen } from 'lucide-react'
import { forwardRef, useImperativeHandle, useRef } from 'react'

import { Button } from '@/components/ui/button'
import { EquipmentEditor, type EquipmentEditorHandle } from '@/components/inventory/equipment-editor'
import { EquipmentIndex } from '@/components/inventory/equipment-index'
import { InventoryNav, type InventorySectionId } from '@/components/inventory/inventory-nav'
import { LocationsSection } from '@/components/inventory/locations-section'
import { ProfilesSection } from '@/components/inventory/profiles-section'
import { INVENTORY_HELP_TARGETS, type HelpTarget } from '@/lib/help-links'

// ADR 0123: InventoryNav plus whichever section is active - the Settings
// page shape (settings-page.tsx) exactly: App owns the active section (and,
// here, the Equipment index/editor split) and mirrors it to the URL (ADR
// 0074); this component is a thin composition shell with no fetches or
// navigation state of its own.
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
  /** Index "New item". */
  onNewEquipment: () => void
  /** The editor's Back - the one exit that can discard an unsaved draft. */
  onCloseEditor: () => void
  /** A create that already succeeded; carries the id the server assigned. */
  onEquipmentCreated: (id: string) => void
  /** A delete that already succeeded. */
  onEquipmentDeleted: () => void
  onDirtyChange?: (dirty: boolean) => void
  onOpenHelp?: (target: HelpTarget) => void
  canWrite?: boolean
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
    onDirtyChange,
    onOpenHelp,
    canWrite = true,
  },
  ref,
) {
  // Only ever mounted while the Equipment editor itself is - save() is a
  // no-op (App.tsx's guard only ever calls it while inventoryDirty is true,
  // which itself can only be true while the editor is mounted and reporting
  // it) when nothing is bound, same contract as SettingsPageHandle/
  // DocumentDetailsPageHandle's own imperative save.
  const editorRef = useRef<EquipmentEditorHandle>(null)
  useImperativeHandle(ref, () => ({
    save: async () => { await editorRef.current?.save() },
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
            onDirtyChange={onDirtyChange}
            canWrite={canWrite}
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
    if (activeSectionId === 'profiles') return <ProfilesSection canWrite={canWrite} />
    if (activeSectionId === 'locations') return <LocationsSection canWrite={canWrite} />
    return null
  })()

  return (
    <div className="flex flex-col gap-4 md:flex-row">
      <InventoryNav activeSectionId={activeSectionId} onSelect={onSectionChange} />

      <div className="min-w-0 flex-1 space-y-4">
        {onOpenHelp && (
          <div className="mx-auto flex max-w-3xl justify-end">
            <Button
              variant="ghost"
              className="h-10 gap-2 text-primary"
              aria-label="Open help for this section"
              onClick={() => onOpenHelp(INVENTORY_HELP_TARGETS[activeSectionId])}
            >
              <BookOpen className="h-4 w-4" />
              Help
            </Button>
          </div>
        )}
        {activeSection}
      </div>
    </div>
  )
})
