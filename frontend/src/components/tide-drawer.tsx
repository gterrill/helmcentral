import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { formatRefreshAge } from '@/components/forecast-drawer'
import { TideChart } from '@/components/tide-chart'
import { TideStationPicker } from '@/components/tide-station-picker'
import { useTideChart } from '@/hooks/use-tide-chart'
import { useTideSettings } from '@/hooks/use-tide-settings'

const STORM_GLASS_STATION_ID = 'vessel-position'

interface TideDrawerProps {
  isImperial: boolean
}

export function TideDrawer({ isImperial }: TideDrawerProps) {
  const {
    tideProvider,
    tideStationId,
    tideStationName,
    loading: settingsLoading,
    saving,
    error: settingsError,
    saveStation,
  } = useTideSettings()
  const [showPicker, setShowPicker] = useState(false)
  const [autoDetecting, setAutoDetecting] = useState(true)
  const [autoDetectFailed, setAutoDetectFailed] = useState(false)

  useEffect(() => {
    if (settingsLoading || tideProvider !== 'bom' || tideStationId) {
      setAutoDetecting(false)
      return
    }
    fetch('/api/tide-nearest')
      .then((r) => r.ok ? r.json() : Promise.reject(new Error('unavailable')))
      .then((s: { station_id: string; name: string }) => saveStation(s.station_id, s.name))
      .catch(() => setAutoDetectFailed(true))
      .finally(() => setAutoDetecting(false))
  }, [settingsLoading, tideProvider, tideStationId, saveStation])

  const effectiveStationId = tideProvider === 'stormglass' ? STORM_GLASS_STATION_ID : tideStationId
  // Don't start fetching until settings have loaded — prevents the stormglass default from
  // briefly appearing when the configured provider is BOM.
  const { chart, loading, error, isCached, updatedAt, ttlSeconds, refetch } = useTideChart(
    settingsLoading ? '' : tideProvider,
    settingsLoading ? '' : effectiveStationId,
  )

  if (settingsLoading) {
    return (
      <div className="rounded-lg border bg-background/60 px-4 py-8 text-center">
        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Tides</p>
        <p className="mt-2 font-medium text-foreground">Loading tide settings...</p>
      </div>
    )
  }

  if (tideProvider === 'bom' && !tideStationId) {
    if (autoDetecting) {
      return (
        <div className="rounded-lg border bg-background/60 px-4 py-8 text-center">
          <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Tides</p>
          <p className="mt-2 font-medium text-foreground">Finding nearest tide station…</p>
        </div>
      )
    }
    return (
      <div className="space-y-3">
        <div className="rounded-lg border bg-background/60 px-4 py-8 text-center">
          <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Tides</p>
          <p className="mt-2 font-medium text-foreground">
            {autoDetectFailed
              ? 'Could not detect vessel position — choose a tide station'
              : 'Choose a tide station to see a forecast chart'}
          </p>
        </div>
        <TideStationPicker
          provider={tideProvider}
          onSelect={(station) => void saveStation(station.stationId, station.name)}
        />
        {saving && <p className="text-center text-xs text-muted-foreground">Saving station...</p>}
        {settingsError && <p className="text-center text-xs text-destructive">{settingsError}</p>}
      </div>
    )
  }

  const displayStationName =
    tideProvider === 'stormglass'
      ? (chart?.station.name || 'Current Vessel Position')
      : (tideStationName || chart?.station.name || '')
  const displayStationState = chart?.station.state

  return (
    <div className="space-y-4 pb-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground">Tide Station</p>
          <p className="text-lg font-semibold text-foreground">
            {displayStationName}
            {displayStationState && <span className="ml-1 text-sm text-muted-foreground">({displayStationState})</span>}
          </p>
        </div>
        {tideProvider === 'bom' && (
          <Button type="button" size="sm" variant="outline" className="h-8 text-xs" onClick={() => setShowPicker((prev) => !prev)}>
            Change Station
          </Button>
        )}
      </div>

      {showPicker && (
        <TideStationPicker
          provider={tideProvider}
          onSelect={(station) => {
            setShowPicker(false)
            void saveStation(station.stationId, station.name)
          }}
        />
      )}

      <div className="rounded-md border bg-card/70 p-2">
        {loading && !chart ? (
          <p className="py-6 text-center text-xs text-muted-foreground">Loading tide data...</p>
        ) : error && !chart ? (
          <div className="py-6 text-center">
            <p className="text-xs text-destructive">{error}</p>
            <Button type="button" size="sm" variant="outline" className="mt-3 h-8" onClick={() => void refetch()}>
              Retry
            </Button>
          </div>
        ) : chart ? (
          <TideChart chart={chart} isImperial={isImperial} />
        ) : (
          <p className="py-6 text-center text-xs text-muted-foreground">No tide data available</p>
        )}
      </div>

      {(isCached || updatedAt) && (
        <p className="text-[10px] text-muted-foreground">
          Data: {tideProvider === 'bom' ? 'Bureau of Meteorology (Australia)' : 'Storm Glass'}
          {' · '}
          {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
          {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 3600)}h` : ''}
        </p>
      )}

      {saving && <p className="text-xs text-muted-foreground">Saving station...</p>}
      {settingsError && <p className="text-xs text-destructive">{settingsError}</p>}
    </div>
  )
}
