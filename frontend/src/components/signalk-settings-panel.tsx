import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { SettingsInput } from '@/components/ui/settings-input'
import { SettingsSelect } from '@/components/ui/settings-select'

type SettingsPayload = {
  signalk?: {
    address?: string
    port?: number
  }
  boat?: {
    name?: string
    model?: string
    house_battery_capacity_ah?: number
  }
  ui?: {
    vessel_state_refresh_seconds?: number
    tank_labels?: Record<string, string>
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

export function SignalKSettingsPanel() {
  const [signalKAddress, setSignalKAddress] = useState('localhost')
  const [signalKPort, setSignalKPort] = useState('3000')

  const [boatName, setBoatName] = useState('')
  const [boatModel, setBoatModel] = useState('')
  const [houseBatteryCapacityAh, setHouseBatteryCapacityAh] = useState('1440')
  const [distanceUnits, setDistanceUnits] = useState<'metric' | 'imperial'>('metric')
  const [vesselStateRefreshSeconds, setVesselStateRefreshSeconds] = useState('10')
  const [tankLabels, setTankLabels] = useState<Record<string, string>>(
    Object.fromEntries(defaultTankLabelIds.map((id) => [id, ''])),
  )

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

    if (typeof data.boat?.name === 'string') {
      setBoatName(data.boat.name)
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
    if (data.ui?.tank_labels && typeof data.ui.tank_labels === 'object') {
      setTankLabels((previous) => ({
        ...previous,
        ...data.ui?.tank_labels,
      }))
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
        name: boatName.trim(),
        model: boatModel.trim(),
        house_battery_capacity_ah: parseNumber(houseBatteryCapacityAh, 1440),
      },
      units: distanceUnits,
      ui: {
        vessel_state_refresh_seconds: Math.round(parseNumber(vesselStateRefreshSeconds, 10)),
        tank_labels: tankLabels,
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
      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">SignalK Connection</h3>

        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,180px)_auto]">
          <SettingsInput
            label="Address"
            value={signalKAddress}
            onChange={setSignalKAddress}
            ariaLabel="SignalK address"
          />

          <SettingsInput
            label="Port"
            value={signalKPort}
            onChange={setSignalKPort}
            ariaLabel="SignalK port"
          />

          <Button
            variant="outline"
            className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
            onClick={connectSignalK}
            disabled={isConnecting}
          >
            {isConnecting ? 'Connecting' : 'Connect'}
          </Button>

          <SettingsInput
            label="Refresh Seconds"
            value={vesselStateRefreshSeconds}
            onChange={setVesselStateRefreshSeconds}
            ariaLabel="Vessel state refresh seconds"
            className="md:col-span-3"
          />
        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Boat And UI</h3>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <SettingsInput
            label="Boat Name"
            value={boatName}
            onChange={setBoatName}
            ariaLabel="Boat name"
          />

          <SettingsInput
            label="Boat Model"
            value={boatModel}
            onChange={setBoatModel}
            ariaLabel="Boat model"
          />

          <SettingsInput
            label="Battery Ah"
            value={houseBatteryCapacityAh}
            onChange={setHouseBatteryCapacityAh}
            ariaLabel="House battery capacity"
          />

          <SettingsSelect
            label="Units"
            value={distanceUnits}
            onChange={setDistanceUnits}
            options={[
              { value: 'metric', label: 'Metric' },
              { value: 'imperial', label: 'Imperial' },
            ]}
            ariaLabel="Distance units"
          />
        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Labels</h3>
        <div className="mt-3 grid grid-cols-1 gap-2">
          {tankLabelIds.map((id) => (
            <SettingsInput
              key={id}
              label={`Tank Label ${id}`}
              value={tankLabels[id] ?? ''}
              onChange={(val) => setTankLabel(id, val)}
              ariaLabel={`Tank label ${id}`}
            />
          ))}
        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Anchor</h3>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <SettingsInput
            label="Bow Roller M"
            value={bowRollerHeightM}
            onChange={setBowRollerHeightM}
            ariaLabel="Bow roller height"
          />

          <SettingsInput
            label="Chain Size Mm"
            value={chainSizeMm}
            onChange={setChainSizeMm}
            ariaLabel="Chain size"
          />

          <SettingsInput
            label="Chain Onboard M"
            value={chainOnboardM}
            onChange={setChainOnboardM}
            ariaLabel="Chain onboard length"
          />

          <SettingsInput
            label="Windage M2"
            value={windageAreaM2}
            onChange={setWindageAreaM2}
            ariaLabel="Windage area"
          />

          <SettingsSelect
            label="Hull Type"
            value={hullType}
            onChange={setHullType}
            options={[
              { value: 'power_cat', label: 'power_cat' },
              { value: 'sail_mono', label: 'sail_mono' },
              { value: 'power_mono', label: 'power_mono' },
              { value: 'sail_cat', label: 'sail_cat' },
            ]}
            ariaLabel="Hull type"
            isMultiCol
          />
        </div>
      </div>

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
