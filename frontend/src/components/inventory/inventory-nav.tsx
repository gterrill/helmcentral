import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

// ADR 0123: the Inventory panel's internal section nav, the exact SettingsNav
// shape (settings-nav.tsx) - Card, buttons, aria-current - not a new pattern.
// App owns which section is active and mirrors it to the URL (ADR 0074), the
// same contract settingsSection already has.

export type InventorySectionId = 'equipment' | 'profiles' | 'locations' | 'stocktake'

export const INVENTORY_SECTIONS: Array<{ id: InventorySectionId; label: string }> = [
  { id: 'equipment', label: 'Equipment' },
  { id: 'profiles', label: 'Profiles' },
  { id: 'locations', label: 'Locations' },
  // ADR 0127 (the plan's Phase B): NFC and keyboard-wedge scanning, not the
  // RFID hardware ADR 0065 §4 originally designed for - that stays
  // designed-not-built (docs/features/inventory-tracking.md says so).
  { id: 'stocktake', label: 'Stocktake' },
]

interface InventoryNavProps {
  activeSectionId: InventorySectionId
  onSelect: (id: InventorySectionId) => void
}

export function InventoryNav({ activeSectionId, onSelect }: InventoryNavProps) {
  return (
    <Card className="w-full gap-0 py-2 md:w-48">
      <CardContent className="flex flex-row gap-1 overflow-x-auto px-2 md:flex-col md:overflow-visible">
        {INVENTORY_SECTIONS.map((section) => (
          <button
            key={section.id}
            type="button"
            onClick={() => onSelect(section.id)}
            aria-current={activeSectionId === section.id ? 'true' : undefined}
            className={cn(
              'whitespace-nowrap rounded-md px-3 py-2 text-left text-xs font-medium uppercase tracking-[0.08em] transition-colors',
              activeSectionId === section.id
                ? 'bg-primary/10 text-primary'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {section.label}
          </button>
        ))}
      </CardContent>
    </Card>
  )
}
