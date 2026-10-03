import { FormSection, SettingsLayout } from '@/components/patterns'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { ProviderGroup } from '@/components/settings/provider-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { usePlaceNameProviders } from '@/hooks/use-place-name-providers'
import { usePOIProviders } from '@/hooks/use-poi-providers'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'

interface PluginsSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

// One headed section per plugin kind, read top to bottom, in the order the
// operator thinks about the weather: forecast, sea, tide, warnings, then the
// two place-related kinds. Each description names what in Helmcentral goes
// quiet or changes when that kind's plugin is switched, so the choice is made
// knowing what it touches.
export function PluginsSection({ draft, onChange }: PluginsSectionProps) {
  const weather = useWeatherProviders()
  const wave = useWaveProviders()
  const tide = useTideProviders()
  const forecastWarnings = useForecastWarningsProviders()
  const placeNames = usePlaceNameProviders()
  const poi = usePOIProviders()

  return (
    <SettingsLayout title="Plugins">
      <FormSection
        title="Weather"
        description="The weather and wind forecast in the Forecast panel and on the Today & Now, Current Conditions, Forecast Conditions and Clock tiles, Mate's forecasts, and the sunrise and sunset times the Auto theme follows."
      >
        <ProviderGroup
          type="weather"
          providers={weather.providers}
          loading={weather.loading}
          error={weather.error}
          kind="weather plugins"
          emptyMessage="No weather plugin installed."
        />
      </FormSection>

      <FormSection
        title="Wave"
        description="The wave and swell forecast and sea temperature: the sea state in the Forecast panel, the wave chart on Forecast Conditions, sea temperature on Today & Now, and the sea state in Mate's forecasts and passage estimates."
      >
        <ProviderGroup
          type="wave"
          providers={wave.providers}
          loading={wave.loading}
          error={wave.error}
          kind="wave plugins"
          emptyMessage="No wave plugin installed."
        />
      </FormSection>

      <FormSection
        title="Tide"
        description="Tide heights and times for the station below: the Depth & Tide tile, the Forecast panel's tide chart, the tide line and low-water clearance warning in Anchor Watch, and the rode planner's depth at high water. Mate's tide and fair-tide departure answers use the station nearest the place you ask about."
      >
        <ProviderGroup
          type="tide"
          providers={tide.providers}
          loading={tide.loading}
          error={tide.error}
          kind="tide plugins"
          emptyMessage="No tide plugin installed."
        />

        <div className="grid grid-cols-1 gap-4 border-t border-border pt-4 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="tide-station-id">Tide Station Id</FieldLabel>
            <Input
              id="tide-station-id"
              value={draft.tideStationId}
              onChange={(e) => onChange({ tideStationId: e.target.value })}
              aria-label="Tide station id"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="tide-station-name">Tide Station Name</FieldLabel>
            <Input
              id="tide-station-name"
              value={draft.tideStationName}
              onChange={(e) => onChange({ tideStationName: e.target.value })}
              aria-label="Tide station name"
            />
          </Field>

          <Field orientation="horizontal" className="md:col-span-2">
            <Switch
              checked={draft.tideAutoStation}
              onCheckedChange={(checked) => onChange({ tideAutoStation: checked })}
            />
            <FieldLabel>Auto-update tide station as vessel moves</FieldLabel>
          </Field>
        </div>
      </FormSection>

      <FormSection
        title="Forecast Warnings"
        description="Official marine warnings for the boat's forecast area. They show in the alarm banner, the Alarms panel and the Forecast panel, raise the wind, gale and surf forecast-warning alarms, and are passed to Mate."
      >
        <ProviderGroup
          type="forecast-warnings"
          providers={forecastWarnings.providers}
          loading={forecastWarnings.loading}
          error={forecastWarnings.error}
          kind="forecast warnings plugins"
          emptyMessage="No forecast warnings plugin installed."
        />
      </FormSection>

      <FormSection
        title="Place names"
        description="Names where the boat is: the Position and Clock tiles, the anchorage name in Anchor Watch, the name of a destination set on the chartplotter, and where each AIS contact was seen. Mate uses it to find a place you name."
      >
        <ProviderGroup
          type="place-names"
          providers={placeNames.providers}
          loading={placeNames.loading}
          error={placeNames.error}
          kind="place-name plugins"
          emptyMessage="No installed plugin can look up place names. Place names come from a points-of-interest plugin that supports them."
        />
      </FormSection>

      <FormSection
        title="Nearby"
        description="Marinas, fuel, anchorages and other points of interest on the Nearby tile. Nothing else uses it."
      >
        <ProviderGroup
          type="poi"
          providers={poi.providers}
          loading={poi.loading}
          error={poi.error}
          kind="points-of-interest plugins"
          emptyMessage="No points-of-interest plugin installed."
        />
      </FormSection>
    </SettingsLayout>
  )
}
