import { SectionNav, type SectionNavGroup } from '@/components/section-nav'

// ADR 0123: the Inventory panel's internal section nav, a thin wrapper over
// the shared SectionNav (section-nav.tsx) the exact shape settings-nav.tsx
// wraps too - not a new pattern. App owns which section is active and
// mirrors it to the URL (ADR 0074), the same contract settingsSection
// already has. Inventory has one job with four parts rather than groups of
// jobs, so it is a single unlabelled group.

export type InventorySectionId = 'equipment' | 'profiles' | 'locations' | 'stocktake'

const INVENTORY_SECTION_GROUPS: Array<SectionNavGroup<InventorySectionId>> = [
  {
    items: [
      { id: 'equipment', label: 'Equipment' },
      { id: 'profiles', label: 'Profiles' },
      { id: 'locations', label: 'Locations' },
      // ADR 0127 (the plan's Phase B): NFC and keyboard-wedge scanning, not the
      // RFID hardware ADR 0065 §4 originally designed for - that stays
      // designed-not-built (docs/features/inventory-tracking.md says so).
      { id: 'stocktake', label: 'Stocktake' },
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
