import { memo, useEffect, useMemo, useState } from 'react'

import { Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { formatRefreshAge } from '@/components/forecast-drawer'
import { TideChart, TidePhaseBadge } from '@/components/tide-chart'
import { TideStationPicker } from '@/components/tide-station-picker'
import { useTideChart } from '@/hooks/use-tide-chart'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { classifyTidePhase } from '@/lib/tide-phase'

const STORM_GLASS_STATION_ID = 'vessel-position'

interface ForecastTideSectionProps {
  isImperial: boolean
  dayOffset: number // 0 = today, matches ForecastDrawer's selectedDayIndex
}

export const ForecastTideSection = memo(function ForecastTideSection({ isImperial, dayOffset }: ForecastTideSectionProps) {
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
    if (settingsLoading || tideProvider === 'stormglass' || tideStationId) {
      setAutoDetecting(false)
      return
    }
    fetch(`/api/tide-nearest?provider=${tideProvider}`)
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

  const { windowStart, windowEnd } = useMemo(() => {
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    start.setDate(start.getDate() + dayOffset)
    const end = new Date(start)
    end.setDate(end.getDate() + 1)
    return { windowStart: start, windowEnd: end }
  }, [dayOffset])

  return (
    <div className="mt-3 rounded-md border bg-card/70 p-2">
      <h4 className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <Waves size={13} className="text-gauge-secondary" /> Tide
      </h4>

      {settingsLoading ? (
        <p className="py-6 text-center text-xs text-muted-foreground">Loading tide settings...</p>
      ) : tideProvider === 'bom' && !tideStationId ? (
        autoDetecting ? (
          <p className="py-6 text-center text-xs text-muted-foreground">Finding nearest tide station…</p>
        ) : (
          <div className="space-y-3">
            <p className="py-6 text-center text-xs text-muted-foreground">
              {autoDetectFailed
                ? 'Could not detect vessel position — choose a tide station'
                : 'Choose a tide station to see a forecast chart'}
            </p>
            <TideStationPicker
              provider={tideProvider}
              onSelect={(station) => void saveStation(station.stationId, station.name)}
            />
            {saving && <p className="text-center text-xs text-muted-foreground">Saving station...</p>}
            {settingsError && <p className="text-center text-xs text-destructive">{settingsError}</p>}
          </div>
        )
      ) : (
        <>
          {(() => {
            const displayStationName =
              tideProvider === 'stormglass'
                ? (chart?.station.name || 'Current Vessel Position')
                : (tideStationName || chart?.station.name || '')
            const displayStationState = chart?.station.state
            const tidePhase = chart ? classifyTidePhase(chart.extremes, new Date()) : null

            return (
              <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p className="text-lg font-semibold text-foreground">
                    {displayStationName}
                    {displayStationState && <span className="ml-1 text-sm text-muted-foreground">({displayStationState})</span>}
                  </p>
                  {tidePhase && <TidePhaseBadge phase={tidePhase} className="mt-0.5" />}
                </div>
                {tideProvider === 'bom' && (
                  <Button type="button" size="sm" variant="outline" className="h-8 text-xs" onClick={() => setShowPicker((prev) => !prev)}>
                    Change Station
                  </Button>
                )}
              </div>
            )
          })()}

          {showPicker && (
            <TideStationPicker
              provider={tideProvider}
              onSelect={(station) => {
                setShowPicker(false)
                void saveStation(station.stationId, station.name)
              }}
            />
          )}

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
            <TideChart chart={chart} isImperial={isImperial} windowStart={windowStart} windowEnd={windowEnd} />
          ) : (
            <p className="py-6 text-center text-xs text-muted-foreground">No tide data available</p>
          )}

          {(isCached || updatedAt) && (
            <p className="mt-1 text-xs text-muted-foreground">
              Data: {tideProvider === 'bom' ? 'Bureau of Meteorology (Australia)' : 'Storm Glass'}
              {' · '}
              {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
              {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 3600)}h` : ''}
            </p>
          )}

          {saving && <p className="mt-1 text-xs text-muted-foreground">Saving station...</p>}
          {settingsError && <p className="mt-1 text-xs text-destructive">{settingsError}</p>}
        </>
      )}
    </div>
  )
})
