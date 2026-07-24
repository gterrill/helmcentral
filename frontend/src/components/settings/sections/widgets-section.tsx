import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ProviderGroup } from '@/components/settings/provider-group'
import { useSettingsFormContext } from '@/components/settings/settings-form-context'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { useForecastWarningsProviders } from '@/hooks/use-forecast-warnings-providers'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useWaveProviders } from '@/hooks/use-wave-providers'
import { useWeatherProviders } from '@/hooks/use-weather-providers'

interface WidgetsSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function WidgetsSection({ draft, onChange }: WidgetsSectionProps) {
  const { settings } = useSettingsFormContext()
  const { providers: tideProviders } = useTideProviders()
  const { providers: weatherProviders } = useWeatherProviders()
  const { providers: waveProviders } = useWaveProviders()
  const { providers: forecastWarningsProviders } = useForecastWarningsProviders()

  // Tide has no "active if unset" default (see provider-group.tsx), so an
  // unconfigured tide provider is genuinely empty here, not 'bom'.
  const tideProvider = settings.ui?.tide_provider || ''

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Widgets</FieldLegend>

        <Tabs defaultValue="tide">
          <TabsList>
            <TabsTrigger value="tide">Tide</TabsTrigger>
            <TabsTrigger value="weather">Weather</TabsTrigger>
            <TabsTrigger value="wave">Wave</TabsTrigger>
            <TabsTrigger value="forecast-warnings">Forecast Warnings</TabsTrigger>
          </TabsList>

          <TabsContent value="tide" className="space-y-3">
            <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
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

              {tideProvider === 'bom' && (
                <Field orientation="horizontal" className="md:col-span-2">
                  <Switch
                    checked={draft.tideAutoStation}
                    onCheckedChange={(checked) => onChange({ tideAutoStation: checked })}
                  />
                  <FieldLabel>Auto-update tide station as vessel moves</FieldLabel>
                </Field>
              )}
            </div>

            <ProviderGroup type="tide" providers={tideProviders} />
          </TabsContent>

          <TabsContent value="weather">
            <ProviderGroup type="weather" providers={weatherProviders} />
          </TabsContent>

          <TabsContent value="wave">
            <ProviderGroup type="wave" providers={waveProviders} />
          </TabsContent>

          <TabsContent value="forecast-warnings">
            <ProviderGroup type="forecast-warnings" providers={forecastWarningsProviders} />
          </TabsContent>
        </Tabs>
      </FieldSet>
    </div>
  )
}
