import { memo, useEffect, useMemo, useState } from 'react'

import { Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { formatRefreshAge } from '@/components/forecast-drawer'
import { TideChart, TidePhaseBadge } from '@/components/tide-chart'
import { TideStationPicker } from '@/components/tide-station-picker'
import { apiRequest } from '@/lib/api-query'
import { useTideChart } from '@/hooks/use-tide-chart'
import { useTideProviders } from '@/hooks/use-tide-providers'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { usePlaceTideStation } from '@/hooks/use-place-tide-station'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { addDaysISO } from '@/lib/stay'
import { formatDayKey, selectTimeZone, unknownZoneMessage, zonedDayStart } from '@/lib/vessel-time'
import { isFarFromPlace, TIDE_STATION_FAR_NM } from '@/lib/stay'
import { classifyTidePhase } from '@/lib/tide-phase'

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
  const { providers: tideProviders } = useTideProviders()
  const timeZone = useVesselStateSelector(selectTimeZone)
  const [showPicker, setShowPicker] = useState(false)
  const [autoDetecting, setAutoDetecting] = useState(true)
  const [autoDetectFailed, setAutoDetectFailed] = useState(false)

  useEffect(() => {
    if (settingsLoading || tideStationId) {
      setAutoDetecting(false)
      return
    }
    apiRequest<{ station_id: string; name: string }>(`/api/tide-nearest?provider=${tideProvider}`)
      .then((s) => saveStation(s.station_id, s.name))
      .catch(() => setAutoDetectFailed(true))
      .finally(() => setAutoDetecting(false))
  }, [settingsLoading, tideProvider, tideStationId, saveStation])

  // Don't start fetching until settings have loaded — prevents a stale/default
  // provider or station from briefly flashing before the real configured
  // values are known.
  const { chart, loading, error, isCached, updatedAt, ttlSeconds, refetch } = useTideChart(
    settingsLoading ? '' : tideProvider,
    settingsLoading ? '' : tideStationId,
  )

  const { windowStart, windowEnd } = useMemo(() => {
    // The boat's day, not the device's: "today" is the date in the boat's zone.
    if (!timeZone || unknownZoneMessage(timeZone)) return { windowStart: null, windowEnd: null }
    const day = addDaysISO(formatDayKey(new Date(), timeZone), dayOffset)
    const start = zonedDayStart(day, timeZone)
    return { windowStart: start, windowEnd: zonedDayStart(addDaysISO(day, 1), timeZone) }
  }, [dayOffset, timeZone])

  // Rendered beside TideChart's legend at the bottom (same row, right-aligned)
  // once a chart exists to host it; falls back to the section's own bottom
  // when there is no station, the fetch is still loading or failed, or the
  // day has no chart data at all - matching the Cloud/Wind/Wave cards'
  // "still show the meta even with no legend to share a row with" rule.
  const tideRefreshMeta = (isCached || updatedAt) ? (
    <p data-testid="forecast-tide-refresh-meta" className="text-sm text-muted-foreground">
      Data: {tideProviders.find((p) => p.id === tideProvider)?.name || tideProvider}
      {' · '}
      {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
      {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 3600)}h` : ''}
    </p>
  ) : null

  return (
    <div className="mt-3 rounded-md border bg-card/70 p-2">
      <h4 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {/* Tide is a single-axis chart, so the unit belongs here rather than
            on the chart's ticks - the same rule Wind (kts) and Wave (m)
            follow. See the axis-idiom rule block in forecast-drawer.tsx. */}
        <Waves size={13} className="text-gauge-secondary" /> Tide ({isImperial ? 'ft' : 'm'})
      </h4>

      {settingsLoading ? (
        <p className="py-6 text-center text-xs text-muted-foreground">Loading tide settings...</p>
      ) : !tideStationId ? (
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
            const displayStationName = tideStationName || chart?.station.name || ''
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
                <Button type="button" size="sm" variant="outline" className="h-8 text-xs" onClick={() => setShowPicker((prev) => !prev)}>
                  Change Station
                </Button>
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
          ) : unknownZoneMessage(timeZone) ? (
            <p data-testid="forecast-tide-zone-error" className="py-6 text-center text-xs text-destructive">{unknownZoneMessage(timeZone)}</p>
          ) : chart && windowStart && windowEnd && timeZone ? (
            <TideChart chart={chart} isImperial={isImperial} windowStart={windowStart} windowEnd={windowEnd} footerMeta={tideRefreshMeta} timeZone={timeZone} />
          ) : (
            <p className="py-6 text-center text-xs text-muted-foreground">No tide data available</p>
          )}

          {saving && <p className="mt-1 text-xs text-muted-foreground">Saving station...</p>}
          {settingsError && <p className="mt-1 text-xs text-destructive">{settingsError}</p>}
        </>
      )}

      {/* TideChart hosts tideRefreshMeta itself once a chart exists (beside
          its legend); every other branch above has no legend row to share,
          so it surfaces here instead of disappearing. */}
      {!chart && tideRefreshMeta && <div className="mt-1 flex justify-end">{tideRefreshMeta}</div>}
    </div>
  )
})

interface ForecastPlaceTideSectionProps {
  isImperial: boolean
  place: { name: string; lat: number; lon: number; timezone: string }
  /** The place's calendar day being read, YYYY-MM-DD. */
  dayKey: string
}

/**
 * The tide for the stay view: the station nearest the place, for this view
 * only. It reads the boat's tide provider but never writes a station, so the
 * saved station and the auto-station setting stay what the operator chose for
 * the boat. The station is named with how far it is from the place, and the
 * distance is flagged when a tide there could differ from the tide at the
 * place.
 */
export const ForecastPlaceTideSection = memo(function ForecastPlaceTideSection({ isImperial, place, dayKey }: ForecastPlaceTideSectionProps) {
  const { tideProvider, loading: settingsLoading } = useTideSettings()
  const { providers: tideProviders } = useTideProviders()
  const provider = settingsLoading ? '' : tideProvider
  const { nearest, loading: nearestLoading, error: nearestError } = usePlaceTideStation(provider, { lat: place.lat, lon: place.lon })
  const { chart, loading, error, isCached, updatedAt, ttlSeconds, refetch } = useTideChart(provider, nearest?.station.stationId ?? '')

  const { windowStart, windowEnd } = useMemo(() => {
    if (unknownZoneMessage(place.timezone)) return { windowStart: new Date(NaN), windowEnd: new Date(NaN) }
    const start = zonedDayStart(dayKey, place.timezone)
    return { windowStart: start, windowEnd: zonedDayStart(addDaysISO(dayKey, 1), place.timezone) }
  }, [dayKey, place.timezone])
  const zoneError = unknownZoneMessage(place.timezone)
  const timeZone = place.timezone

  const refreshMeta = (isCached || updatedAt) ? (
    <p data-testid="forecast-tide-refresh-meta" className="text-sm text-muted-foreground">
      Data: {tideProviders.find((p) => p.id === tideProvider)?.name || tideProvider}
      {' · '}
      {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
      {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 3600)}h` : ''}
    </p>
  ) : null

  let body
  if (settingsLoading) {
    body = <p className="py-6 text-center text-xs text-muted-foreground">Loading tide settings...</p>
  } else if (!tideProvider) {
    body = <p className="py-6 text-center text-xs text-muted-foreground">Tide provider is not set up. Choose one in Settings to see tides at {place.name}.</p>
  } else if (nearestLoading) {
    body = <p className="py-6 text-center text-xs text-muted-foreground">Finding the nearest tide station to {place.name}…</p>
  } else if (!nearest) {
    body = (
      <p className="py-6 text-center text-xs text-muted-foreground">
        No tide station found near {place.name}{nearestError ? ` (${nearestError})` : ''}.
      </p>
    )
  } else {
    const far = nearest.distanceNm !== null && isFarFromPlace(nearest.distanceNm)
    const miles = nearest.distanceNm !== null ? nearest.distanceNm.toFixed(1) : null
    const stationName = chart?.station.name || nearest.station.name
    const stationState = chart?.station.state ?? nearest.station.state
    body = (
      <>
        <div className="mb-2">
          <p className="text-lg font-semibold text-foreground">
            {stationName}
            {stationState && <span className="ml-1 text-sm text-muted-foreground">({stationState})</span>}
          </p>
          {miles !== null && (
            <p data-testid="forecast-place-tide-distance" className="text-xs text-muted-foreground">
              {miles} NM from {place.name}
            </p>
          )}
          {far && (
            <p data-testid="forecast-place-tide-far" className="mt-1 text-xs font-medium text-amber-700 dark:text-amber-400">
              Nearest station is {miles} NM away, more than {TIDE_STATION_FAR_NM} NM. Tide times and heights at {place.name} may differ.
            </p>
          )}
        </div>
        {loading && !chart ? (
          <p className="py-6 text-center text-xs text-muted-foreground">Loading tide data...</p>
        ) : error && !chart ? (
          <div className="py-6 text-center">
            <p className="text-xs text-destructive">{error}</p>
            <Button type="button" size="sm" variant="outline" className="mt-3 h-8" onClick={() => void refetch()}>
              Retry
            </Button>
          </div>
        ) : zoneError ? (
          <p data-testid="forecast-place-tide-zone-error" className="py-6 text-center text-xs text-destructive">{zoneError}</p>
        ) : chart ? (
          <TideChart chart={chart} isImperial={isImperial} windowStart={windowStart} windowEnd={windowEnd} footerMeta={refreshMeta} timeZone={timeZone} />
        ) : (
          <p className="py-6 text-center text-xs text-muted-foreground">No tide data available</p>
        )}
      </>
    )
  }

  return (
    <div className="mt-3 rounded-md border bg-card/70 p-2">
      <h4 className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <Waves size={13} className="text-gauge-secondary" /> Tide ({isImperial ? 'ft' : 'm'})
      </h4>
      {body}
      {!chart && refreshMeta && <div className="mt-1 flex justify-end">{refreshMeta}</div>}
    </div>
  )
})
