import { SectionNav, type SectionNavGroup } from '@/components/section-nav'

// ADR 0123: the Inventory panel's internal section nav, a thin wrapper over
// the shared SectionNav (section-nav.tsx) the exact shape settings-nav.tsx
// wraps too - not a new pattern. App owns which section is active and
// mirrors it to the URL (ADR 0074), the same contract settingsSection
// already has. Grouped the way settings-nav.tsx groups: what the boat carries
// and where it is, then what keeps it running.

export type InventorySectionId = 'equipment' | 'maintenance' | 'profiles' | 'locations' | 'stocktake'

const INVENTORY_SECTION_GROUPS: Array<SectionNavGroup<InventorySectionId>> = [
  {
    label: 'Inventory',
    items: [
      { id: 'equipment', label: 'Equipment' },
      { id: 'locations', label: 'Locations' },
      // ADR 0127 (the plan's Phase B): NFC and keyboard-wedge scanning, not the
      // RFID hardware ADR 0065 §4 originally designed for - that stays
      // designed-not-built (docs/features/inventory-tracking.md says so).
      { id: 'stocktake', label: 'Stocktake' },
    ],
  },
  {
    label: 'Servicing',
    items: [
      // ADR 0138: service rules and the log they're completed into - a
      // plain sortable list, not a fifth index/editor split.
      { id: 'maintenance', label: 'Maintenance' },
      { id: 'profiles', label: 'Profiles' },
    ],
  },
]

export const INVENTORY_SECTIONS: Array<{ id: InventorySectionId; label: string }> =
  INVENTORY_SECTION_GROUPS.flatMap((group) => group.items)

interface InventoryNavProps {
  activeSectionId: InventorySectionId
  onSelect: (id: InventorySectionId) => void
}

export function InventoryNav({ activeSectionId, onSelect }: InventoryNavProps) {
  return (
    <SectionNav
      groups={INVENTORY_SECTION_GROUPS}
      activeId={activeSectionId}
      onSelect={onSelect}
      aria-label="Inventory sections"
    />
  )
}
