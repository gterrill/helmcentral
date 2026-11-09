import { memo, useEffect, useMemo, useRef, useState } from 'react'

import { Waves } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { formatRefreshAge } from '@/components/forecast-drawer'
import { TideChart, TidePhaseBadge } from '@/components/tide-chart'
import { TideStationPicker } from '@/components/tide-station-picker'
import { useAnchorWatchSelector, selectAnchorPointKey } from '@/hooks/use-anchor-watch'
import { useConfiguredTideChart } from '@/hooks/use-configured-tide-chart'
import { useTideSettings } from '@/hooks/use-tide-settings'
import { apiRequest } from '@/lib/api-query'
import { effectiveTideProvider, isMeanSeaLevel, MEAN_SEA_LEVEL_LABEL, MEAN_SEA_LEVEL_NOTE } from '@/lib/tide-datum'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { addDaysISO } from '@/lib/stay'
import { formatDayKey, selectTimeZone, unknownZoneMessage, zonedDayStart } from '@/lib/vessel-time'
import { distanceNm, isFarFromPlace, TIDE_STATION_FAR_NM } from '@/lib/stay'
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
  const timeZone = useVesselStateSelector(selectTimeZone)
  const [showPicker, setShowPicker] = useState(false)

  // The server picks the tide (ADR 0273): the provider in force, with its saved
  // station, or for one that follows the boat the anchor's or boat's point. The
  // screen sends neither, so it never handles a station for such a provider and
  // never saves one. The key only makes the chart fetch again when the saved
  // provider or station changes.
  const provider = effectiveTideProvider(tideProvider)
  const anchorPoint = useAnchorWatchSelector(selectAnchorPointKey)
  const { chart, loading, error, noStation, notSetUp, isCached, updatedAt, ttlSeconds, providerName, positionBased, refetch } =
    useConfiguredTideChart(`${provider}|${tideStationId}|${anchorPoint}`, !settingsLoading)

  // Only a provider with stations answers `no_station`, so a station found here
  // is a real one: the one nearest the boat, saved as the operator would have
  // chosen it. A provider that follows a position never gets here, so nothing
  // made up is saved. Saving it changes the chart's key, which fetches it again.
  const [autoDetecting, setAutoDetecting] = useState(false)
  const [autoDetectFailed, setAutoDetectFailed] = useState(false)
  const triedFor = useRef('')
  useEffect(() => {
    if (!noStation) {
      // A station is saved, or the chart came back: a later `no_station` for
      // this provider is asked again.
      triedFor.current = ''
      return
    }
    if (triedFor.current === provider) return
    triedFor.current = provider
    setAutoDetecting(true)
    apiRequest<{ station_id: string; name: string }>(`/api/tide-nearest?provider=${provider}`)
      .then((s) => saveStation(s.station_id, s.name))
      .catch(() => setAutoDetectFailed(true))
      .finally(() => setAutoDetecting(false))
  }, [noStation, provider, saveStation])
  // Labelled from the chart's own datum, so it follows what the server sent.
  const meanSeaLevel = isMeanSeaLevel(chart?.heightDatum)
  // Until the chart (or its error) says what kind of provider this is, neither
  // the saved station nor the station controls are shown for it.
  const followsBoat = positionBased === true
  const hasStations = positionBased === false

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
      Data: {providerName || provider}
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
        <Waves size={13} className="text-gauge-secondary" /> Tide ({isImperial ? 'ft' : 'm'}{meanSeaLevel ? `, ${MEAN_SEA_LEVEL_LABEL}` : ''})
      </h4>

      {settingsLoading ? (
        <p className="py-6 text-center text-xs text-muted-foreground">Loading tide settings...</p>
      ) : noStation && autoDetecting ? (
        <p className="py-6 text-center text-xs text-muted-foreground">Finding nearest tide station…</p>
      ) : noStation ? (
        <div className="space-y-3">
          <p className="py-6 text-center text-xs text-muted-foreground">
            {autoDetectFailed ? 'Could not detect vessel position — choose a tide station' : 'Choose a tide station to see a forecast chart'}
          </p>
          <TideStationPicker
            provider={provider}
            onSelect={(station) => void saveStation(station.stationId, station.name)}
          />
          {saving && <p className="text-center text-xs text-muted-foreground">Saving station...</p>}
          {settingsError && <p className="text-center text-xs text-destructive">{settingsError}</p>}
        </div>
      ) : notSetUp ? (
        <p data-testid="forecast-tide-not-set-up" className="py-6 text-center text-xs text-destructive">{error}</p>
      ) : (
        <>
          {(() => {
            const displayStationName = hasStations ? tideStationName || chart?.station.name || '' : chart?.station.name || ''
            const displayStationState = followsBoat ? undefined : chart?.station.state
            const tidePhase = chart ? classifyTidePhase(chart.extremes, new Date()) : null

            return (
              <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p className="text-lg font-semibold text-foreground">
                    {displayStationName}
                    {displayStationState && <span className="ml-1 text-sm text-muted-foreground">({displayStationState})</span>}
                  </p>
                  {followsBoat && (
                    <p data-testid="forecast-tide-follows-boat" className="text-xs text-muted-foreground">
                      The tide follows the boat's position.{meanSeaLevel ? ` ${MEAN_SEA_LEVEL_NOTE}` : ''}
                    </p>
                  )}
                  {tidePhase && <TidePhaseBadge phase={tidePhase} className="mt-0.5" />}
                </div>
                {hasStations && (
                  <Button type="button" size="sm" variant="outline" className="h-8 text-xs" onClick={() => setShowPicker((prev) => !prev)}>
                    Change Station
                  </Button>
                )}
              </div>
            )
          })()}

          {showPicker && hasStations && (
            <TideStationPicker
              provider={provider}
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
  const provider = effectiveTideProvider(tideProvider)
  // The server finds the tide at the place: the station nearest it, or for a
  // provider that follows a position the place's own point. No station lookup
  // comes first, and no station is ever saved.
  const { chart, loading, error, notSetUp, isCached, updatedAt, ttlSeconds, providerName, positionBased: followsPlace, refetch } =
    useConfiguredTideChart(provider, !settingsLoading, { lat: place.lat, lon: place.lon })
  const meanSeaLevel = isMeanSeaLevel(chart?.heightDatum)

  const { windowStart, windowEnd } = useMemo(() => {
    if (unknownZoneMessage(place.timezone)) return { windowStart: new Date(NaN), windowEnd: new Date(NaN) }
    const start = zonedDayStart(dayKey, place.timezone)
    return { windowStart: start, windowEnd: zonedDayStart(addDaysISO(dayKey, 1), place.timezone) }
  }, [dayKey, place.timezone])
  const zoneError = unknownZoneMessage(place.timezone)
  const timeZone = place.timezone

  const refreshMeta = (isCached || updatedAt) ? (
    <p data-testid="forecast-tide-refresh-meta" className="text-sm text-muted-foreground">
      Data: {providerName || provider}
      {' · '}
      {isCached ? 'cached' : 'live'} · updated {formatRefreshAge(updatedAt, Date.now())}
      {ttlSeconds ? ` · refreshes every ${Math.round(ttlSeconds / 3600)}h` : ''}
    </p>
  ) : null

  let body
  if (settingsLoading) {
    body = <p className="py-6 text-center text-xs text-muted-foreground">Loading tide settings...</p>
  } else if (notSetUp) {
    body = (
      <p data-testid="forecast-place-tide-not-set-up" className="py-6 text-center text-xs text-muted-foreground">
        Tide provider is not set up: {error}. Choose another in Settings to see tides at {place.name}.
      </p>
    )
  } else if (loading && !chart) {
    body = <p className="py-6 text-center text-xs text-muted-foreground">Finding the tide at {place.name}…</p>
  } else if (error && !chart) {
    body = (
      <div className="py-6 text-center">
        <p className="text-xs text-destructive">No tide found near {place.name} ({error}).</p>
        <Button type="button" size="sm" variant="outline" className="mt-3 h-8" onClick={() => void refetch()}>
          Retry
        </Button>
      </div>
    )
  } else {
    // A tide that follows the place has no station to be a distance away. A
    // station the server named without a position is never measured against 0,0.
    const located = chart !== null && !(chart.station.lat === 0 && chart.station.lon === 0)
    const miles = !followsPlace && chart && located ? distanceNm(place.lat, place.lon, chart.station.lat, chart.station.lon) : null
    const far = miles !== null && isFarFromPlace(miles)
    const stationName = followsPlace ? place.name : chart?.station.name
    const stationState = followsPlace ? undefined : chart?.station.state
    body = (
      <>
        <div className="mb-2">
          <p className="text-lg font-semibold text-foreground">
            {stationName}
            {stationState && <span className="ml-1 text-sm text-muted-foreground">({stationState})</span>}
          </p>
          {followsPlace && (
            <p data-testid="forecast-place-tide-modelled" className="text-xs text-muted-foreground">
              The tide is modelled for this place.{meanSeaLevel ? ` ${MEAN_SEA_LEVEL_NOTE}` : ''}
            </p>
          )}
          {miles !== null && (
            <p data-testid="forecast-place-tide-distance" className="text-xs text-muted-foreground">
              {miles.toFixed(1)} NM from {place.name}
            </p>
          )}
          {far && (
            <p data-testid="forecast-place-tide-far" className="mt-1 text-xs font-medium text-amber-700 dark:text-amber-400">
              Nearest station is {miles.toFixed(1)} NM away, more than {TIDE_STATION_FAR_NM} NM. Tide times and heights at {place.name} may differ.
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
        <Waves size={13} className="text-gauge-secondary" /> Tide ({isImperial ? 'ft' : 'm'}{meanSeaLevel ? `, ${MEAN_SEA_LEVEL_LABEL}` : ''})
      </h4>
      {body}
      {!chart && refreshMeta && <div className="mt-1 flex justify-end">{refreshMeta}</div>}
    </div>
  )
})
