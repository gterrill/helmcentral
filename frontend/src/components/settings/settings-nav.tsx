import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export type SettingsSectionId =
  | 'general'
  | 'signalk'
  | 'boat-ui'
  | 'widgets'
  | 'influxdb'
  | 'geonames'
  | 'anchor-watch'
  | 'security'

export const SETTINGS_SECTIONS: Array<{ id: SettingsSectionId; label: string }> = [
  { id: 'general', label: 'General' },
  { id: 'boat-ui', label: 'Vessel' },
  { id: 'signalk', label: 'SignalK' },
  { id: 'influxdb', label: 'InfluxDB' },
  { id: 'anchor-watch', label: 'Anchor Watch' },
  { id: 'widgets', label: 'Widgets' },
  { id: 'security', label: 'Security' },
  { id: 'geonames', label: 'GeoNames' },
]

interface SettingsNavProps {
  activeSectionId: SettingsSectionId
  onSelect: (id: SettingsSectionId) => void
}

/**
 * Small vertical section nav for the settings page. Pure local-state swap
 * — no scroll-spy, no URL state — same "swap not scroll" idiom App.tsx
 * already uses for its own top-level panel switch.
 */
export function SettingsNav({ activeSectionId, onSelect }: SettingsNavProps) {
  return (
    <Card className="w-full gap-0 py-2 md:w-48">
      <CardContent className="flex flex-row gap-1 overflow-x-auto px-2 md:flex-col md:overflow-visible">
        {SETTINGS_SECTIONS.map((section) => (
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
