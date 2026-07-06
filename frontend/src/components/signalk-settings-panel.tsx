import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { useTideProviders } from '@/hooks/use-tide-providers'

type SettingsPayload = {
  signalk?: {
    address?: string
    port?: number
  }
  boat?: {
    vessel_prefix?: string
    model?: string
    house_battery_capacity_ah?: number
  }
  ui?: {
    vessel_state_refresh_seconds?: number
    forecast_refresh_seconds?: number
    tank_labels?: Record<string, string>
    tide_provider?: string
    tide_station_id?: string
    tide_station_name?: string
    tide_auto_station?: boolean
  }
  anchor?: {
    bow_roller_height_m?: number
    chain_size_mm?: number
    chain_onboard_m?: number
    hull_type?: string
    windage_area_m2?: number
  }
  units?: string
}

const defaultTankLabelIds = [
  'blackWater.1',
  'blackWater.6',
  'freshWater.0',
  'freshWater.3',
  'fuel.2',
  'fuel.4',
  'fuel.5',
  'fuel.7',
]

interface SignalKSettingsPanelProps {
  autoCloseAnchorWatchEnabled?: boolean
  onAutoCloseAnchorWatchToggle?: (enabled: boolean) => void
  theme?: string
  onThemeChange?: (theme: string) => void
}

export function SignalKSettingsPanel({
  autoCloseAnchorWatchEnabled = true,
  onAutoCloseAnchorWatchToggle,
  theme = 'marine',
  onThemeChange,
}: SignalKSettingsPanelProps) {
  const [signalKAddress, setSignalKAddress] = useState('localhost')
  const [signalKPort, setSignalKPort] = useState('3000')

  const [vesselPrefix, setVesselPrefix] = useState('')
  const [boatModel, setBoatModel] = useState('')
  const [houseBatteryCapacityAh, setHouseBatteryCapacityAh] = useState('1440')
  const [distanceUnits, setDistanceUnits] = useState<'metric' | 'imperial'>('metric')
  const [vesselStateRefreshSeconds, setVesselStateRefreshSeconds] = useState('10')
  const [forecastRefreshSeconds, setForecastRefreshSeconds] = useState('3600')
  const [tankLabels, setTankLabels] = useState<Record<string, string>>(
    Object.fromEntries(defaultTankLabelIds.map((id) => [id, ''])),
  )
  const [tideProvider, setTideProvider] = useState('stormglass')
  const [tideStationId, setTideStationId] = useState('')
  const [tideStationName, setTideStationName] = useState('')
  const [tideAutoStation, setTideAutoStation] = useState(false)
  const { providers: tideProviders } = useTideProviders()

  const [bowRollerHeightM, setBowRollerHeightM] = useState('1.5')
  const [chainSizeMm, setChainSizeMm] = useState('12')
  const [chainOnboardM, setChainOnboardM] = useState('150')
  const [hullType, setHullType] = useState<'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat'>('power_cat')
  const [windageAreaM2, setWindageAreaM2] = useState('35')

  const [isConnecting, setIsConnecting] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [connectError, setConnectError] = useState<string | null>(null)
  const [connectSuccess, setConnectSuccess] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saveSuccess, setSaveSuccess] = useState<string | null>(null)

  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  const applySettings = (data: SettingsPayload) => {
    if (data.signalk?.address) {
      setSignalKAddress(data.signalk.address)
    }
    if (typeof data.signalk?.port === 'number') {
      setSignalKPort(String(data.signalk.port))
    }

    if (typeof data.boat?.vessel_prefix === 'string') {
      setVesselPrefix(data.boat.vessel_prefix)
    }
    if (typeof data.boat?.model === 'string') {
      setBoatModel(data.boat.model)
    }
    if (typeof data.boat?.house_battery_capacity_ah === 'number') {
      setHouseBatteryCapacityAh(String(data.boat.house_battery_capacity_ah))
    }

    if (data.units === 'metric' || data.units === 'imperial') {
      setDistanceUnits(data.units)
    }
    if (typeof data.ui?.vessel_state_refresh_seconds === 'number') {
      setVesselStateRefreshSeconds(String(data.ui.vessel_state_refresh_seconds))
    }
    if (typeof data.ui?.forecast_refresh_seconds === 'number') {
      setForecastRefreshSeconds(String(data.ui.forecast_refresh_seconds))
    }
    if (data.ui?.tank_labels && typeof data.ui.tank_labels === 'object') {
      setTankLabels((previous) => ({
        ...previous,
        ...data.ui?.tank_labels,
      }))
    }
    if (typeof data.ui?.tide_provider === 'string' && data.ui.tide_provider !== '') {
      setTideProvider(data.ui.tide_provider)
    }
    if (typeof data.ui?.tide_station_id === 'string') {
      setTideStationId(data.ui.tide_station_id)
    }
    if (typeof data.ui?.tide_station_name === 'string') {
      setTideStationName(data.ui.tide_station_name)
    }
    if (typeof data.ui?.tide_auto_station === 'boolean') {
      setTideAutoStation(data.ui.tide_auto_station)
    }

    if (typeof data.anchor?.bow_roller_height_m === 'number') {
      setBowRollerHeightM(String(data.anchor.bow_roller_height_m))
    }
    if (typeof data.anchor?.chain_size_mm === 'number') {
      setChainSizeMm(String(data.anchor.chain_size_mm))
    }
    if (typeof data.anchor?.chain_onboard_m === 'number') {
      setChainOnboardM(String(data.anchor.chain_onboard_m))
    }
    if (
      data.anchor?.hull_type === 'power_cat'
      || data.anchor?.hull_type === 'sail_mono'
      || data.anchor?.hull_type === 'power_mono'
      || data.anchor?.hull_type === 'sail_cat'
    ) {
      setHullType(data.anchor.hull_type)
    }
    if (typeof data.anchor?.windage_area_m2 === 'number') {
      setWindageAreaM2(String(data.anchor.windage_area_m2))
    }
  }

  useEffect(() => {
    const fetchSettings = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings`)
        if (!response.ok) {
          throw new Error('Failed to fetch settings')
        }

        const data = (await response.json()) as SettingsPayload
        applySettings(data)
      } catch {
        try {
          const response = await fetch(`${apiBaseUrl}/api/settings/signalk`)
          if (!response.ok) {
            throw new Error('Failed to fetch SignalK settings')
          }
          const data = (await response.json()) as { address?: string; port?: number }
          applySettings({ signalk: data })
        } catch {
          // Keep defaults if settings endpoint is unavailable.
        }
      }
    }

    void fetchSettings()
  }, [apiBaseUrl])

  const parseNumber = (value: string, fallback: number) => {
    const parsed = Number.parseFloat(value)
    return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
  }

  const connectSignalK = async () => {
    setIsConnecting(true)
    setConnectError(null)
    setConnectSuccess(null)

    const normalizedPort = Number.parseInt(signalKPort, 10)
    const payload = {
      address: signalKAddress.trim(),
      port: Number.isFinite(normalizedPort) ? normalizedPort : 3000,
    }

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings/signalk`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to connect to SignalK')
      }

      const data = (await response.json()) as {
        address?: string
        port?: number
      }

      if (data.address) {
        setSignalKAddress(data.address)
      }
      if (data.port) {
        setSignalKPort(String(data.port))
      }

      setConnectSuccess('SignalK settings updated')
    } catch (error) {
      setConnectError(error instanceof Error ? error.message : 'Unable to connect to SignalK')
    } finally {
      setIsConnecting(false)
    }
  }

  const saveSettings = async () => {
    setIsSaving(true)
    setSaveError(null)
    setSaveSuccess(null)

    const payload: SettingsPayload = {
      signalk: {
        address: signalKAddress.trim(),
        port: Number.parseInt(signalKPort, 10) || 3000,
      },
      boat: {
        vessel_prefix: vesselPrefix.trim(),
        model: boatModel.trim(),
        house_battery_capacity_ah: parseNumber(houseBatteryCapacityAh, 1440),
      },
      units: distanceUnits,
      ui: {
        vessel_state_refresh_seconds: Math.round(parseNumber(vesselStateRefreshSeconds, 10)),
        forecast_refresh_seconds: Math.round(parseNumber(forecastRefreshSeconds, 3600)),
        tank_labels: tankLabels,
        tide_provider: tideProvider,
        tide_station_id: tideStationId,
        tide_station_name: tideStationName,
        tide_auto_station: tideAutoStation,
      },
      anchor: {
        bow_roller_height_m: parseNumber(bowRollerHeightM, 1.5),
        chain_size_mm: parseNumber(chainSizeMm, 12),
        chain_onboard_m: parseNumber(chainOnboardM, 150),
        hull_type: hullType,
        windage_area_m2: parseNumber(windageAreaM2, 35),
      },
    }

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to save settings')
      }

      const data = (await response.json()) as SettingsPayload
      applySettings(data)
      setSaveSuccess('Settings saved')
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : 'Unable to save settings')
    } finally {
      setIsSaving(false)
    }
  }

  const setTankLabel = (id: string, value: string) => {
    setTankLabels((previous) => ({
      ...previous,
      [id]: value,
    }))
  }

  const tankLabelIds = Object.keys(tankLabels).sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">SignalK Connection</FieldLegend>

        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,180px)_auto]">
          <Field>
            <FieldLabel htmlFor="signalk-address">Address</FieldLabel>
            <Input
              id="signalk-address"
              value={signalKAddress}
              onChange={(e) => setSignalKAddress(e.target.value)}
              aria-label="SignalK address"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="signalk-port">Port</FieldLabel>
            <Input
              id="signalk-port"
              value={signalKPort}
              onChange={(e) => setSignalKPort(e.target.value)}
              aria-label="SignalK port"
            />
          </Field>

          <Button
            variant="outline"
            className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
            onClick={connectSignalK}
            disabled={isConnecting}
          >
            {isConnecting ? 'Connecting' : 'Connect'}
          </Button>

          <Field className="md:col-span-3">
            <FieldLabel htmlFor="signalk-refresh-seconds">Refresh Seconds</FieldLabel>
            <Input
              id="signalk-refresh-seconds"
              value={vesselStateRefreshSeconds}
              onChange={(e) => setVesselStateRefreshSeconds(e.target.value)}
              aria-label="Vessel state refresh seconds"
            />
          </Field>

          <Field className="md:col-span-3">
            <FieldLabel htmlFor="forecast-refresh-seconds">Forecast Refresh Seconds</FieldLabel>
            <Input
              id="forecast-refresh-seconds"
              value={forecastRefreshSeconds}
              onChange={(e) => setForecastRefreshSeconds(e.target.value)}
              aria-label="Forecast refresh seconds"
            />
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Boat And UI</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="vessel-prefix">Vessel Prefix</FieldLabel>
            <Input
              id="vessel-prefix"
              value={vesselPrefix}
              onChange={(e) => setVesselPrefix(e.target.value)}
              aria-label="Vessel prefix"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="boat-model">Boat Model</FieldLabel>
            <Input
              id="boat-model"
              value={boatModel}
              onChange={(e) => setBoatModel(e.target.value)}
              aria-label="Boat model"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="battery-ah">Battery Ah</FieldLabel>
            <Input
              id="battery-ah"
              value={houseBatteryCapacityAh}
              onChange={(e) => setHouseBatteryCapacityAh(e.target.value)}
              aria-label="House battery capacity"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="distance-units">Units</FieldLabel>
            <Select
              value={distanceUnits}
              onValueChange={(value) => value && setDistanceUnits(value)}
            >
              <SelectTrigger id="distance-units" aria-label="Distance units">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="metric">Metric</SelectItem>
                <SelectItem value="imperial">Imperial</SelectItem>
              </SelectPopup>
            </Select>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Tide</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="tide-provider">Tide Provider</FieldLabel>
            <Select
              value={tideProvider}
              onValueChange={(value) => value && setTideProvider(value)}
            >
              <SelectTrigger id="tide-provider" aria-label="Tide provider">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                {tideProviders.map((provider) => (
                  <SelectItem key={provider.id} value={provider.id}>
                    {provider.name}
                  </SelectItem>
                ))}
              </SelectPopup>
            </Select>
          </Field>

          {tideProvider === 'bom' && (
            <Field orientation="horizontal" className="md:col-span-2">
              <Switch checked={tideAutoStation} onCheckedChange={setTideAutoStation} />
              <FieldLabel>Auto-update tide station as vessel moves</FieldLabel>
            </Field>
          )}
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Labels</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2">
          {tankLabelIds.map((id) => (
            <Field key={id}>
              <FieldLabel htmlFor={`tank-label-${id}`}>{`Tank Label ${id}`}</FieldLabel>
              <Input
                id={`tank-label-${id}`}
                value={tankLabels[id] ?? ''}
                onChange={(e) => setTankLabel(id, e.target.value)}
                aria-label={`Tank label ${id}`}
              />
            </Field>
          ))}
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Anchor</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="bow-roller-height">Bow Roller M</FieldLabel>
            <Input
              id="bow-roller-height"
              value={bowRollerHeightM}
              onChange={(e) => setBowRollerHeightM(e.target.value)}
              aria-label="Bow roller height"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-size">Chain Size Mm</FieldLabel>
            <Input
              id="chain-size"
              value={chainSizeMm}
              onChange={(e) => setChainSizeMm(e.target.value)}
              aria-label="Chain size"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="chain-onboard">Chain Onboard M</FieldLabel>
            <Input
              id="chain-onboard"
              value={chainOnboardM}
              onChange={(e) => setChainOnboardM(e.target.value)}
              aria-label="Chain onboard length"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="windage-area">Windage M2</FieldLabel>
            <Input
              id="windage-area"
              value={windageAreaM2}
              onChange={(e) => setWindageAreaM2(e.target.value)}
              aria-label="Windage area"
            />
          </Field>

          <Field className="md:col-span-2">
            <FieldLabel htmlFor="hull-type">Hull Type</FieldLabel>
            <Select value={hullType} onValueChange={(value) => value && setHullType(value)}>
              <SelectTrigger id="hull-type" aria-label="Hull type">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="power_cat">power_cat</SelectItem>
                <SelectItem value="sail_mono">sail_mono</SelectItem>
                <SelectItem value="power_mono">power_mono</SelectItem>
                <SelectItem value="sail_cat">sail_cat</SelectItem>
              </SelectPopup>
            </Select>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Anchor Watch Options</FieldLegend>
        <div className="mt-3 space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={autoCloseAnchorWatchEnabled}
              onCheckedChange={(checked) => onAutoCloseAnchorWatchToggle?.(checked)}
            />
            <FieldLabel>
              Auto-close anchor watch when engines start (if outside circle for 5+ seconds)
            </FieldLabel>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Appearance</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="ui-theme">Theme</FieldLabel>
            <Select value={theme} onValueChange={(v) => onThemeChange?.(v as string)}>
              <SelectTrigger id="ui-theme" aria-label="UI theme">
                <SelectValue />
              </SelectTrigger>
              <SelectPopup>
                <SelectItem value="marine">Marine — Aldrich / Barlow</SelectItem>
                <SelectItem value="classic">Classic — Playfair / Lato</SelectItem>
                <SelectItem value="clean">Clean — Inter</SelectItem>
              </SelectPopup>
            </Select>
          </Field>
        </div>
      </FieldSet>

      <div className="flex justify-end">
        <Button
          variant="outline"
          className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
          onClick={saveSettings}
          disabled={isSaving}
        >
          {isSaving ? 'Saving' : 'Save Settings'}
        </Button>
      </div>

      {connectError && (
        <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {connectError}
        </div>
      )}
      {connectSuccess && (
        <div className="mt-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
          {connectSuccess}
        </div>
      )}
      {saveError && (
        <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {saveError}
        </div>
      )}
      {saveSuccess && (
        <div className="mt-3 rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
          {saveSuccess}
        </div>
      )}
    </div>
  )
}
