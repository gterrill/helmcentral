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

interface InventoryPanelProps {
  activeSectionId: InventorySectionId
  onSectionChange: (id: InventorySectionId) => void
  equipmentEditId: string | null
  onEquipmentEditIdChange: (id: string | null) => void
  creatingEquipment: boolean
  onCreatingEquipmentChange: (creating: boolean) => void
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
    onEquipmentEditIdChange,
    creatingEquipment,
    onCreatingEquipmentChange,
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
            onBack={() => { onEquipmentEditIdChange(null); onCreatingEquipmentChange(false) }}
            onCreated={(id) => { onCreatingEquipmentChange(false); onEquipmentEditIdChange(id) }}
            onDeleted={() => { onEquipmentEditIdChange(null); onCreatingEquipmentChange(false) }}
            onDirtyChange={onDirtyChange}
            canWrite={canWrite}
          />
        )
      }
      return (
        <EquipmentIndex
          onOpenItem={(id) => onEquipmentEditIdChange(id)}
          onNewItem={() => onCreatingEquipmentChange(true)}
          canWrite={canWrite}
        />
      )
    }
    if (activeSectionId === 'profiles') return <ProfilesSection />
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
