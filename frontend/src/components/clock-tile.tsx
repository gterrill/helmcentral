import { memo, useCallback, useMemo } from 'react'
import { Sunrise, Sunset } from 'lucide-react'

import { Tile } from '@/components/ui/tile'
import { formatClock, formatDate, formatWeekday, isSameLocalDate } from '@/lib/vessel-time'
import { moonPhaseEmoji, moonPhaseLabel } from '@/lib/moon-phase'
import { FORECAST_REFRESH_SECONDS, PLACE_NAME_REFRESH_SECONDS } from '@/config/app-config'
import { usePlaceName } from '@/hooks/use-place-name'
import { useRouteActivation, type ActiveRouteStatus } from '@/hooks/use-route-activation'
import { useVesselClock } from '@/hooks/use-vessel-clock'
import { useVesselStateSelector, type VesselStateValue } from '@/hooks/use-vessel-state'
import { useForecastSun } from '@/hooks/use-weather-forecast'
import { computeClockTripEta } from '@/lib/next-waypoint'

/**
 * The clock tile's trip ETA (ADR 0092, ADR 0125): the whole trip's arrival,
 * not just the next leg. label is the FINAL waypoint's name for a
 * Helmcentral-activated route (etaToRouteEnd, lib/next-waypoint.ts), or a
 * bare chartplotter go-to destination's reverse-geocoded name
 * (etaToDestination) when no Helmcentral route is active - `null` when
 * that destination has no name yet (a fresh backend cache miss, or a
 * provider that never resolves one), in which case the ETA line shows just
 * the time rather than inventing a label.
 */
export interface ClockTileTripEta {
  label: string | null
  /** null when SOG can't produce an honest ETA (e.g. becalmed, or a bare destination with no SOG to steer by). */
  etaAt: Date | null
}

export interface ClockTileProps {
  /** Backend-formatted, e.g. "6:02AM" (forecast[0].sunriseTime). Null when the forecast has no fix for it. */
  sunriseTime: string | null
  sunsetTime: string | null
  moonPhase: string | null
  placeName: string | null
  tripEta: ClockTileTripEta | null
  /** The boat's time, to the second. Null until the server has sent one. */
  now: Date | null
  /** `now` as formatClock gives it in the vessel's zone; null with it. */
  clock: ReturnType<typeof formatClock> | null
  /** The vessel's IANA zone; undefined until the server has sent one. */
  timeZone: string | undefined
  /** Where the zone came from: a live fix, the last fix, or none yet (UTC). Null until the server says. */
  timeZoneSource: VesselStateValue['timeZoneSource']
  /** Set when this browser does not know the boat's zone; shown where the zone note goes. */
  zoneError?: string | null
}

/** Drops :SS from a formatClock timePart ("14:32:07" -> "14:32"). */
function hhmm(timePart: string) {
  const [hh = '--', mm = '--'] = timePart.split(':')
  return `${hh}:${mm}`
}

/**
 * The wall display's clock page (ADR 0092, ADR 0125): time, date, sun and
 * moon, and where the boat is and where it's headed. It takes the boat's time
 * as props; LiveClockTile (below) reads the clock, so this is the
 * one tile on the wall that re-renders every second and every other tile
 * stays clear of it (ADR 0218).
 *
 * The hero time is one step down from the drawer's own convention
 * (`text-6xl`, not `text-7xl`) and the place chip carries the ETA as a
 * second line rather than a separate margined block below it: at this
 * tile's placed height (h7 on the wall, minH 6 as its resize floor) the
 * original three-block, `text-7xl` layout measured taller than the tile
 * itself in a real browser (jsdom has no layout engine, so nothing in this
 * suite caught it) and clipped the ETA line off the bottom of the wall.
 * Both trims are votes for keeping the ETA genuinely visible over squeezing
 * out a few more pixels of hero digit.
 */
export const ClockTile = memo(function ClockTile({
  sunriseTime,
  sunsetTime,
  moonPhase,
  placeName,
  tripEta,
  now,
  clock,
  timeZone,
  timeZoneSource,
  zoneError = null,
}: ClockTileProps) {
  // The header (vessel-status-bar.tsx) shares formatDate's full-weekday output via
  // currentDate; this tile is two grid columns wide (~300px) and a long weekday
  // ("SATURDAY, SEP 12, 2026") wraps to a second line there, pushing the place
  // chip below the tile's bottom edge. The compact option keeps formatDate's
  // weekday-first ordering but stays short enough to hold one line at minW.
  const compactDate = now === null ? '--' : formatDate(now, { compact: true, timeZone }).toUpperCase()

  const zoneNote = zoneError ? zoneError : timeZoneSource === 'last_fix' ? 'last fix' : timeZoneSource === 'none' ? 'UTC · no position yet' : null

  const etaLabel = (() => {
    if (!tripEta) return '—'
    if (!tripEta.etaAt) return tripEta.label ? `ETA ${tripEta.label} —` : 'ETA —'

    // HelmCast prefixed a multi-day passage's ETA with the arrival weekday
    // rather than leaving it looking like it lands today - carried forward
    // here now the trip ETA can span more than one day.
    // A zone this browser does not know has no clock to put the ETA on.
    if (zoneError) return `ETA ${tripEta.label ? `${tripEta.label} ` : ''}--`
    const dayPrefix = now === null || isSameLocalDate(tripEta.etaAt, now, timeZone) ? '' : `${formatWeekday(tripEta.etaAt, timeZone)} `
    // Include the meridiem the same way the hero clock does (its own
    // `clock.meridiem` span) - a bare "09:40" can't be told from 9:40 PM,
    // and this line has no other AM/PM context of its own to lean on.
    const etaClock = formatClock(tripEta.etaAt, timeZone)
    const time = `${dayPrefix}${hhmm(etaClock.timePart)}${etaClock.meridiem ? ` ${etaClock.meridiem}` : ''}`
    const labelPart = tripEta.label ? `${tripEta.label} ` : ''
    return `ETA ${labelPart}${time}`
  })()

  return (
    <Tile title="Clock">
      <div className="mt-1 flex h-full flex-col justify-between gap-1.5">
        <div>
          <time className="flex items-baseline gap-1 font-display leading-none tabular-nums text-gauge-secondary">
            <span className="text-6xl">{hhmm(clock?.timePart ?? '--:--')}</span>
            <span className="text-2xl">{clock?.meridiem}</span>
            {/* A live fix needs no caveat. Boat time kept from the last fix, or UTC
                because no position has ever been seen (ADR 0230), explains an
                otherwise wrong-looking clock. */}
            {zoneNote && (
              <span
                data-testid="clock-timezone-label"
                className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground"
              >
                {zoneNote}
              </span>
            )}
          </time>
          <p
            data-testid="clock-date"
            className="mt-1 truncate whitespace-nowrap text-xl uppercase tracking-[0.08em] text-muted-foreground"
          >
            {compactDate}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-3 text-lg tabular-nums">
          <span className="flex items-center gap-1.5">
            <Sunrise className="h-4 w-4 text-gauge-primary" aria-hidden="true" />
            {sunriseTime ?? '—'}
          </span>
          <span className="flex items-center gap-1.5">
            <Sunset className="h-4 w-4 text-gauge-primary" aria-hidden="true" />
            {sunsetTime ?? '—'}
          </span>
          <span className="flex items-center gap-1.5">
            <span aria-hidden="true">{moonPhase ? moonPhaseEmoji(moonPhase) : '—'}</span>
            {moonPhase ? moonPhaseLabel(moonPhase) : null}
          </span>
        </div>

        {/* Place chip and trip ETA share one box (see the component doc
            comment above) rather than the ETA sitting in its own margined
            paragraph below it - the merge is what buys back the vertical
            room the layout fix needs. */}
        <div className="truncate rounded-md bg-gauge-secondary/10 px-3 py-1 leading-tight">
          <div className="truncate font-display text-lg text-gauge-secondary">{placeName ?? '—'}</div>
          <p data-testid="clock-eta" className="truncate text-xs text-muted-foreground">
            {etaLabel}
          </p>
        </div>
      </div>
    </Tile>
  )
})

interface ClockInputs {
  hasFix: boolean
  latitude: number | null
  longitude: number | null
  speedOverGroundKts: number | null
}

function sameClockInputs(a: ClockInputs, b: ClockInputs): boolean {
  return a.hasFix === b.hasFix && a.latitude === b.latitude && a.longitude === b.longitude && a.speedOverGroundKts === b.speedOverGroundKts
}

/** Whether there is a trip to time: an active route, or a go-to the Course API names. */
function hasTrip(status: ActiveRouteStatus): boolean {
  return status.state === 'active' || (status.state === 'inactive' && status.destination !== null)
}

/**
 * The clock tile: today's sun and moon from the forecast, the place name, and
 * the trip ETA (ADR 0092, ADR 0125).
 * computeClockTripEta reports to the final waypoint of the route SignalK is
 * navigating, or to the Course API's bare next point when a chartplotter
 * go-to has no route behind it.
 *
 * The ETA needs the fix and speed only while there is a trip. Without one the
 * tile reads just whether there is a fix, which is all the place name asks,
 * so a clock on a boat at anchor does not redraw on every fix. The wrapper
 * also reads the boat's clock (useVesselClock), so it re-renders each second,
 * and so does the tile it draws; nothing else on the page does.
 */
export const LiveClockTile = memo(function LiveClockTile() {
  const { status: routeStatus } = useRouteActivation()
  const { sunriseTime, sunsetTime, moonPhase } = useForecastSun(FORECAST_REFRESH_SECONDS)
  const trip = hasTrip(routeStatus)
  const select = useCallback((state: VesselStateValue): ClockInputs => ({
    hasFix: state.latitude !== null && state.longitude !== null,
    latitude: trip ? state.latitude : null,
    longitude: trip ? state.longitude : null,
    speedOverGroundKts: trip ? state.speedOverGroundKts : null,
  }), [trip])
  const { hasFix, latitude, longitude, speedOverGroundKts } = useVesselStateSelector(select, sameClockInputs)
  const placeName = usePlaceName(hasFix, PLACE_NAME_REFRESH_SECONDS)
  const tripEta = useMemo(
    () => computeClockTripEta(routeStatus, latitude, longitude, speedOverGroundKts, new Date()),
    [routeStatus, latitude, longitude, speedOverGroundKts],
  )
  const { now, clock, timeZone, timeZoneSource, zoneError } = useVesselClock()
  return <ClockTile sunriseTime={sunriseTime} sunsetTime={sunsetTime} moonPhase={moonPhase} placeName={placeName} tripEta={tripEta} now={now} clock={clock} timeZone={timeZone} timeZoneSource={timeZoneSource} zoneError={zoneError} />
})
