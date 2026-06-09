import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'

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
          <label className="flex min-w-0 items-center gap-2 rounded-md border border-primary/40 bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Address</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={signalKAddress}
              onChange={(event) => setSignalKAddress(event.target.value)}
              aria-label="SignalK address"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Port</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={signalKPort}
              onChange={(event) => setSignalKPort(event.target.value)}
              aria-label="SignalK port"
            />
          </label>

          <Button
            variant="outline"
            className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
            onClick={connectSignalK}
            disabled={isConnecting}
          >
            {isConnecting ? 'Connecting' : 'Connect'}
          </Button>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em] md:col-span-3">
            <span className="text-muted-foreground">Refresh Seconds</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={vesselStateRefreshSeconds}
              onChange={(event) => setVesselStateRefreshSeconds(event.target.value)}
              aria-label="Vessel state refresh seconds"
            />
          </label>
        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Boat And UI</h3>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Boat Name</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={boatName}
              onChange={(event) => setBoatName(event.target.value)}
              aria-label="Boat name"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Boat Model</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={boatModel}
              onChange={(event) => setBoatModel(event.target.value)}
              aria-label="Boat model"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Battery Ah</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={houseBatteryCapacityAh}
              onChange={(event) => setHouseBatteryCapacityAh(event.target.value)}
              aria-label="House battery capacity"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Units</span>
            <select
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={distanceUnits}
              onChange={(event) => setDistanceUnits(event.target.value as 'metric' | 'imperial')}
              aria-label="Distance units"
            >
              <option value="metric">Metric</option>
              <option value="imperial">Imperial</option>
            </select>
          </label>

        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Labels</h3>
        <div className="mt-3 grid grid-cols-1 gap-2">
          {tankLabelIds.map((id) => (
            <label
              key={id}
              className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]"
            >
              <span className="text-muted-foreground">Tank Label {id}</span>
              <input
                className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
                value={tankLabels[id] ?? ''}
                onChange={(event) => setTankLabel(id, event.target.value)}
                aria-label={`Tank label ${id}`}
              />
            </label>
          ))}
        </div>
      </div>

      <div>
        <h3 className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Anchor</h3>
        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-2">
          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Bow Roller M</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={bowRollerHeightM}
              onChange={(event) => setBowRollerHeightM(event.target.value)}
              aria-label="Bow roller height"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Chain Size Mm</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={chainSizeMm}
              onChange={(event) => setChainSizeMm(event.target.value)}
              aria-label="Chain size"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Chain Onboard M</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={chainOnboardM}
              onChange={(event) => setChainOnboardM(event.target.value)}
              aria-label="Chain onboard length"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em]">
            <span className="text-muted-foreground">Windage M2</span>
            <input
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={windageAreaM2}
              onChange={(event) => setWindageAreaM2(event.target.value)}
              aria-label="Windage area"
            />
          </label>

          <label className="flex min-w-0 items-center gap-2 rounded-md border border-border bg-background/80 px-3 py-2 text-xs uppercase tracking-[0.1em] md:col-span-2">
            <span className="text-muted-foreground">Hull Type</span>
            <select
              className="min-w-0 w-full bg-transparent text-sm font-semibold text-foreground outline-none"
              value={hullType}
              onChange={(event) => setHullType(event.target.value as 'power_cat' | 'sail_mono' | 'power_mono' | 'sail_cat')}
              aria-label="Hull type"
            >
              <option value="power_cat">power_cat</option>
              <option value="sail_mono">sail_mono</option>
              <option value="power_mono">power_mono</option>
              <option value="sail_cat">sail_cat</option>
            </select>
          </label>
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
