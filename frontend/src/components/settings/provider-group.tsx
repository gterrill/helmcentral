import { useState } from 'react'

import { ProviderIntegrationCard } from '@/components/settings/provider-integration-card'
import { ProviderSettingsModal, type ProviderDomain } from '@/components/settings/provider-settings-modal'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'

// ProviderGroupDomain extends the settings-modal's own ProviderDomain with
// "place-names" (ADR 0101): a place-names card is always backed by a POI
// plugin (there is no separate plugin kind for it), but ui.place_name_provider
// is its own setting, independent of ui.poi_provider - so it needs its own
// tab/settings-key/default here without inventing a plugin kind the backend
// doesn't have. See MODAL_TYPE_BY_DOMAIN below for how the gear on a
// place-names card still opens the right (poi) settings modal.
export type ProviderGroupDomain = ProviderDomain | 'place-names'

// Weather/wave/forecast-warnings/place-names default to "active" when
// nothing has been configured yet, matching this app's existing hardcoded
// frontend fallback (`useState('open-meteo')` etc. in the old
// signalk-settings-panel.tsx) so a fresh install still shows one card
// active per group. Tide intentionally has NO default (old code:
// `useState('')`) - it can show zero active cards until the operator picks
// one. This asymmetry is deliberate, not a bug.
const DEFAULT_ACTIVE_PROVIDER: Partial<Record<ProviderGroupDomain, string>> = {
  weather: 'open-meteo',
  wave: 'open-meteo-marine',
  poi: 'osm-overpass',
  'forecast-warnings': 'bom',
  'place-names': 'osm-overpass',
}

const SETTINGS_KEY_BY_DOMAIN: Record<
  ProviderGroupDomain,
  | 'tide_provider'
  | 'weather_provider'
  | 'wave_provider'
  | 'poi_provider'
  | 'forecast_warnings_provider'
  | 'place_name_provider'
> = {
  tide: 'tide_provider',
  weather: 'weather_provider',
  wave: 'wave_provider',
  poi: 'poi_provider',
  'forecast-warnings': 'forecast_warnings_provider',
  'place-names': 'place_name_provider',
}

// MODAL_TYPE_BY_DOMAIN maps a ProviderGroup domain to the plugin kind
// ProviderSettingsModal should fetch/save against. Every domain maps to
// itself except "place-names", which always opens the "poi" modal - the
// Overpass server field (or any other POI plugin's config) lives under
// /api/plugins/poi/..., and a place-names card names the very same
// installed POI plugin a Nearby card would, just for a different setting.
const MODAL_TYPE_BY_DOMAIN: Record<ProviderGroupDomain, ProviderDomain> = {
  tide: 'tide',
  weather: 'weather',
  wave: 'wave',
  poi: 'poi',
  'forecast-warnings': 'forecast-warnings',
  'place-names': 'poi',
}

export interface ProviderGroupInfo {
  id: string
  name: string
  description: string
}

interface ProviderGroupProps {
  type: ProviderGroupDomain
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
        type={MODAL_TYPE_BY_DOMAIN[type]}
        providerId={openProviderId}
        open={openProviderId !== null}
        onOpenChange={(open) => {
          if (!open) setOpenProviderId(null)
        }}
      />
    </div>
  )
}
