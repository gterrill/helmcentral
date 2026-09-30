import { SectionNav, type SectionNavGroup } from '@/components/section-nav'

// ADR 0123: 'equipment' (equipment profiles) moved out of this list into
// the Inventory panel's own InventoryNav (inventory-nav.tsx) - profiles are
// reference data about gear, not a setting, and now live beside the
// equipment records that use them rather than here.
export type SettingsSectionId =
  | 'general'
  | 'signalk'
  | 'boat-ui'
  | 'tiles'
  | 'influxdb'
  | 'anchor-watch'
  | 'mayara'
  | 'alarms'
  | 'assistant'
  | 'security'
  | 'logs'
  | 'import'

// Grouped for the shared SectionNav (see its own doc comment): a page with
// several sections of one job groups them by what they're for, not
// alphabetically or by build order. Order is the shipped nav order -
// SETTINGS_SECTIONS below is derived from this, so there is exactly one
// place that decides it.
export const SETTINGS_SECTION_GROUPS: Array<SectionNavGroup<SettingsSectionId>> = [
  {
    label: 'Boat & app',
    items: [
      { id: 'general', label: 'General' },
      { id: 'boat-ui', label: 'Vessel' },
      { id: 'tiles', label: 'Tiles' },
    ],
  },
  {
    label: 'Connections',
    items: [
      { id: 'signalk', label: 'SignalK' },
      { id: 'influxdb', label: 'InfluxDB' },
      { id: 'mayara', label: 'Mayara' },
    ],
  },
  {
    label: 'Features',
    items: [
      { id: 'anchor-watch', label: 'Anchor Watch' },
      { id: 'alarms', label: 'Alarms' },
      { id: 'assistant', label: 'Mate' },
    ],
  },
  {
    label: 'System',
    items: [
      { id: 'security', label: 'Security' },
      { id: 'logs', label: 'Logs' },
      { id: 'import', label: 'Import' },
    ],
  },
]

// The flat list every other caller (help-links.ts, app-location.ts,
// settings-page.tsx's uncontrolled fallback) actually wants - order follows
// the groups above, so there is nowhere else a section can be added and
// silently left out of one list or the other.
export const SETTINGS_SECTIONS: Array<{ id: SettingsSectionId; label: string }> =
  SETTINGS_SECTION_GROUPS.flatMap((group) => group.items)

interface SettingsNavProps {
  activeSectionId: SettingsSectionId
  onSelect: (id: SettingsSectionId) => void
}

/**
 * Settings' in-page section nav - a thin wrapper over the shared SectionNav
 * (section-nav.tsx). Pure controlled list — no state of its own, no
 * scroll-spy. App owns which section is active (ADR 0074) and mirrors it to
 * `/settings/<id>`; SettingsPageContent falls back to its own local state
 * only when no caller is controlling this.
 */
export function SettingsNav({ activeSectionId, onSelect }: SettingsNavProps) {
  return (
    <SectionNav
      groups={SETTINGS_SECTION_GROUPS}
      activeId={activeSectionId}
      onSelect={onSelect}
      aria-label="Settings sections"
    />
  )
}
