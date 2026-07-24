import { useState } from 'react'

import { ProviderIntegrationCard } from '@/components/settings/provider-integration-card'
import { ProviderSettingsModal, type ProviderDomain } from '@/components/settings/provider-settings-modal'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'

// Weather/wave/forecast-warnings default to "active" when nothing has been
// configured yet, matching this app's existing hardcoded frontend fallback
// (`useState('open-meteo')` etc. in the old signalk-settings-panel.tsx) so
// a fresh install still shows one card active per group. Tide intentionally
// has NO default (old code: `useState('')`) — it can show zero active
// cards until the operator picks one. This asymmetry is deliberate, not a
// bug — see the task's final report.
const DEFAULT_ACTIVE_PROVIDER: Partial<Record<ProviderDomain, string>> = {
  weather: 'open-meteo',
  wave: 'open-meteo-marine',
  'forecast-warnings': 'bom',
}

const SETTINGS_KEY_BY_DOMAIN: Record<
  ProviderDomain,
  'tide_provider' | 'weather_provider' | 'wave_provider' | 'forecast_warnings_provider'
> = {
  tide: 'tide_provider',
  weather: 'weather_provider',
  wave: 'wave_provider',
  'forecast-warnings': 'forecast_warnings_provider',
}

export interface ProviderGroupInfo {
  id: string
  name: string
  description: string
}

interface ProviderGroupProps {
  type: ProviderDomain
  providers: ProviderGroupInfo[]
}

/**
 * Renders one domain's grid of provider integration cards. The provider
 * list itself is fetched by the caller (widgets-section.tsx, via the
 * existing `use-{tide,weather,wave,forecast-warnings}-providers` hooks) and
 * passed in as a prop, rather than each of the four ProviderGroup instances
 * calling all four hooks internally — that would quadruple the number of
 * `/api/*-providers` requests for no benefit.
 */
export function ProviderGroup({ type, providers }: ProviderGroupProps) {
  const { settings, save } = useSettingsFormContext()
  const [openProviderId, setOpenProviderId] = useState<string | null>(null)

  const settingsKey = SETTINGS_KEY_BY_DOMAIN[type]
  const configuredId = settings.ui?.[settingsKey]
  const activeId = configuredId && configuredId !== '' ? configuredId : DEFAULT_ACTIVE_PROVIDER[type]

  const handleActivate = (id: string) => {
    void save({ ui: { [settingsKey]: id } })
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {providers.map((provider) => (
          <ProviderIntegrationCard
            key={provider.id}
            id={provider.id}
            name={provider.name}
            description={provider.description}
            active={provider.id === activeId}
            onActivate={handleActivate}
            onOpenSettings={setOpenProviderId}
          />
        ))}
      </div>

      <ProviderSettingsModal
        type={type}
        providerId={openProviderId}
        open={openProviderId !== null}
        onOpenChange={(open) => {
          if (!open) setOpenProviderId(null)
        }}
      />
    </div>
  )
}
