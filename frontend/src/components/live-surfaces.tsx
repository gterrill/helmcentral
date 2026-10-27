import { lazy, memo, useMemo, type ComponentProps } from 'react'

import { AlarmBanner } from '@/components/alarm-banner'
import { AlternatorTile } from '@/components/alternator-tile'
import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AutopilotTile } from '@/components/autopilot-tile'
import { ClockTile } from '@/components/clock-tile'
import { CurrentConditionsTile } from '@/components/current-conditions-tile'
import { DepthTideTile } from '@/components/depth-tide-tile'
import { DisplayShell } from '@/components/display-shell'
import { GeneratorTile } from '@/components/generator-tile'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { PoiMapTile } from '@/components/poi-map-tile'
import { PositionTile } from '@/components/position-tile'
import { RouteTile } from '@/components/route-tile'
import { SignalKDiscoveryPrompt } from '@/components/signalk-discovery-prompt'
import { WindTile } from '@/components/wind-tile'
import { PLACE_NAME_REFRESH_SECONDS } from '@/config/app-config'
import { acknowledgeAlarm, silenceAlarm, useAisCollisionAlarms, useAlarmList } from '@/hooks/use-alarms'
import { anchorWatchAt, type AnchorWatchResult } from '@/hooks/use-anchor-watch'
import { usePlaceName } from '@/hooks/use-place-name'
import type { ActiveRouteStatus } from '@/hooks/use-route-activation'
import { useVesselFields, useVesselStateSelector, type VesselStateValue } from '@/hooks/use-vessel-state'
import { computeClockTripEta } from '@/lib/next-waypoint'

/*
 * The surfaces AppShell renders, wired to the live stores they show (ADR
 * 0215). Each wrapper reads its own slice and hands it to the presentational
 * component as the prop it always took, so those components and their tests
 * stay prop-driven and only these wrappers know a store exists.
 *
 * Each wrapper is memo()'d: its remaining props come from the shell, which
 * passes stable values and callbacks, so a shell render stops here and only a
 * change in the slice it reads re-renders it.
 */

// The drawers are lazy in App.tsx too; these resolve to the same chunks.
const AlarmsDrawer = lazy(() => import('@/components/alarms-drawer').then((mod) => ({ default: mod.AlarmsDrawer })))
const AnchorWatchDrawer = lazy(() => import('@/components/anchor-watch-drawer').then((mod) => ({ default: mod.AnchorWatchDrawer })))
const ForecastPage = lazy(() => import('@/components/forecast-page').then((mod) => ({ default: mod.ForecastPage })))
const RadarDrawer = lazy(() => import('@/components/radar-drawer').then((mod) => ({ default: mod.RadarDrawer })))

type AlarmsDrawerProps = ComponentProps<typeof AlarmsDrawer>
type AnchorWatchDrawerProps = ComponentProps<typeof AnchorWatchDrawer>

/** The banner over every page: the whole active list. */
export const LiveAlarmBanner = memo(function LiveAlarmBanner(props: Omit<ComponentProps<typeof AlarmBanner>, 'alarms' | 'onAcknowledge'>) {
  const alarms = useAlarmList()
  return <AlarmBanner {...props} alarms={alarms} onAcknowledge={acknowledgeAlarm} />
})

/** The Alarms page: the whole active list, and the two actions. */
export const LiveAlarmsDrawer = memo(function LiveAlarmsDrawer(props: Omit<AlarmsDrawerProps, 'alarms' | 'onAcknowledge' | 'onSilence'>) {
  const alarms = useAlarmList()
  return <AlarmsDrawer {...props} alarms={alarms} onAcknowledge={acknowledgeAlarm} onSilence={silenceAlarm} />
})

/**
 * The wall display's frame, for its alarm pill. The pages inside it are
 * `children`, an element the shell built, so an alarm tick that re-renders
 * this frame hands React the same element and the pages are not re-rendered.
 */
export function LiveDisplayShell(props: Omit<ComponentProps<typeof DisplayShell>, 'alarms'>) {
  const alarms = useAlarmList()
  return <DisplayShell {...props} alarms={alarms} />
}

/** The Nearby Vessels list, for the collision highlight on each row. */
export const LiveNearbyVesselsTile = memo(function LiveNearbyVesselsTile(props: Omit<ComponentProps<typeof NearbyVesselsTile>, 'aisCollisionAlarms'>) {
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <NearbyVesselsTile {...props} aisCollisionAlarms={aisCollisionAlarms} />
})

// ── vessel state (ADR 0215 phase 3) ─────────────────────────────────────────
//
// Each field list is a module constant: useVesselFields keys its selection on
// the list, and the tile re-renders only when one of those fields shows
// something new. Ages count only when they cross into stale or change their
// stale label (use-vessel-state.ts).

const WIND_FIELDS = [
  'windLastUpdateAgeS', 'headingTrue', 'windAngleApparentDeg', 'windSide', 'windAngleRelativeDeg',
  'windSpeedApparentKts', 'windSpeedTrueKts', 'windAngleTrueDeg', 'windSideTrue', 'windAngleTrueRelativeDeg',
  'windDirectionTrueDeg', 'currentSetDeg', 'currentDriftKts', 'currentDriftImpactKts', 'maxGustKts', 'maxGustTrueKts',
] as const

/** The Wind tile: wind, heading, current and the gust ladders, all from vessel state. */
export const LiveWindTile = memo(function LiveWindTile() {
  const { windLastUpdateAgeS, ...wind } = useVesselFields(WIND_FIELDS)
  return <WindTile lastUpdateAgeS={windLastUpdateAgeS} {...wind} />
})

const DEPTH_FIELDS = ['depth', 'depthLastUpdateAgeS', 'navigationState'] as const

/** The Depth & Tide tile's depth and navigation state; the tide and trend come from the shell. */
export const LiveDepthTideTile = memo(function LiveDepthTideTile(props: Omit<ComponentProps<typeof DepthTideTile>, 'depth' | 'lastUpdateAgeS' | 'navigationState'>) {
  const { depth, depthLastUpdateAgeS, navigationState } = useVesselFields(DEPTH_FIELDS)
  return <DepthTideTile {...props} depth={depth} lastUpdateAgeS={depthLastUpdateAgeS} navigationState={navigationState} />
})

const POSITION_FIELDS = [
  'positionLastUpdateAgeS', 'latitude', 'longitude', 'headingTrue',
  'gnssValidationState', 'gnssQualityIndicator', 'gnssHdop', 'gnssValidationReason', 'gnssSatellites',
] as const

/** The Position tile: the fix and its quality, and the place name the server looks up for it. */
export const LivePositionTile = memo(function LivePositionTile() {
  const { positionLastUpdateAgeS, ...fix } = useVesselFields(POSITION_FIELDS)
  const placeName = usePlaceName(fix.latitude, fix.longitude, PLACE_NAME_REFRESH_SECONDS)
  return <PositionTile lastUpdateAgeS={positionLastUpdateAgeS} {...fix} placeName={placeName} />
})

const CLOCK_FIELDS = ['latitude', 'longitude', 'speedOverGroundKts'] as const

/**
 * The clock tile: the place name, and the trip ETA (ADR 0092, ADR 0125).
 * computeClockTripEta reports to the final waypoint of the route SignalK is
 * navigating, or to the Course API's bare next point when a chartplotter
 * go-to has no route behind it.
 */
export const LiveClockTile = memo(function LiveClockTile({ routeStatus, ...props }: Omit<ComponentProps<typeof ClockTile>, 'placeName' | 'tripEta'> & { routeStatus: ActiveRouteStatus }) {
  const { latitude, longitude, speedOverGroundKts } = useVesselFields(CLOCK_FIELDS)
  const placeName = usePlaceName(latitude, longitude, PLACE_NAME_REFRESH_SECONDS)
  const tripEta = useMemo(
    () => computeClockTripEta(routeStatus, latitude, longitude, speedOverGroundKts, new Date()),
    [routeStatus, latitude, longitude, speedOverGroundKts],
  )
  return <ClockTile {...props} placeName={placeName} tripEta={tripEta} />
})

const CONDITIONS_FIELDS = ['depth', 'depthLastUpdateAgeS', 'windSpeedTrueKts', 'windDirectionTrueDeg', 'maxGustTrueKts'] as const

/** Current Conditions: depth and true wind from vessel state; the forecast comes from the shell. */
export const LiveCurrentConditionsTile = memo(function LiveCurrentConditionsTile(props: Omit<ComponentProps<typeof CurrentConditionsTile>, 'depth' | 'depthLastUpdateAgeS' | 'windSpeedTrueKts' | 'windDirectionTrueDeg' | 'maxTrueWindKts1h'>) {
  const { depth, depthLastUpdateAgeS, windSpeedTrueKts, windDirectionTrueDeg, maxGustTrueKts } = useVesselFields(CONDITIONS_FIELDS)
  return (
    <CurrentConditionsTile
      {...props}
      depth={depth}
      depthLastUpdateAgeS={depthLastUpdateAgeS}
      windSpeedTrueKts={windSpeedTrueKts}
      windDirectionTrueDeg={windDirectionTrueDeg}
      maxTrueWindKts1h={maxGustTrueKts['1h']}
    />
  )
})

const ROUTE_FIELDS = ['latitude', 'longitude', 'speedOverGroundKts'] as const

/** The Route tile: the route status from the shell, the fix and speed from vessel state. */
export const LiveRouteTile = memo(function LiveRouteTile({ status }: { status: ActiveRouteStatus }) {
  const { latitude, longitude, speedOverGroundKts } = useVesselFields(ROUTE_FIELDS)
  return <RouteTile status={status} latitude={latitude} longitude={longitude} speedKts={speedOverGroundKts ?? 0} />
})

const GENERATOR_FIELDS = ['generatorState', 'generatorManualStart', 'generatorManualStartTimer', 'generatorRunningByCondition', 'generatorRuntime'] as const

/** The Generator tile's own state; its power and the battery come from the electrical stream, still passed by the shell. */
export const LiveGeneratorTile = memo(function LiveGeneratorTile(props: Omit<ComponentProps<typeof GeneratorTile>, (typeof GENERATOR_FIELDS)[number]>) {
  const generator = useVesselFields(GENERATOR_FIELDS)
  return <GeneratorTile {...props} {...generator} />
})

const selectHeading = (state: VesselStateValue) => state.headingTrue

/** The autopilot tile's heading; the autopilot's own state is still passed by the shell. */
export const LiveAutopilotTile = memo(function LiveAutopilotTile(props: Omit<ComponentProps<typeof AutopilotTile>, 'headingTrueDeg'>) {
  const headingTrueDeg = useVesselStateSelector(selectHeading)
  return <AutopilotTile {...props} headingTrueDeg={headingTrueDeg} />
})

const selectEnginesRunning = (state: VesselStateValue) => state.enginesRunning === true

/** The alternator tile shows while an engine runs; the alternator readings are still passed by the shell. */
export const LiveAlternatorTile = memo(function LiveAlternatorTile(props: Omit<ComponentProps<typeof AlternatorTile>, 'enginesRunning'>) {
  const enginesRunning = useVesselStateSelector(selectEnginesRunning)
  return <AlternatorTile {...props} enginesRunning={enginesRunning} />
})

const ANCHOR_FIELDS = [
  'positionLastUpdateAgeS', 'latitude', 'longitude', 'gnssCriticalAlert', 'depth',
  'currentDriftKts', 'currentSetDeg', 'currentDriftImpactKts', 'headingTrue',
] as const

/**
 * The anchor watch tile. The shell hands it the watch record without a fix;
 * this applies the live fix to it (distance, bearing, drag state), and adds
 * depth, current, heading and the collision colours. Its AIS, radar and trail
 * inputs are other streams, still passed by the shell.
 */
export const LiveAnchorWatchTile = memo(function LiveAnchorWatchTile({ watch, ...props }: Omit<ComponentProps<typeof AnchorWatchTile>,
  'aisCollisionAlarms' | 'lastUpdateAgeS' | 'lat' | 'lon' | 'depthMeters' | 'currentDriftKts' | 'currentSetDeg' | 'currentDriftImpactKts' | 'vesselHeadingDeg'>) {
  const v = useVesselFields(ANCHOR_FIELDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useMemo(() => anchorWatchAt(watch, v.latitude, v.longitude, v.gnssCriticalAlert), [watch, v.latitude, v.longitude, v.gnssCriticalAlert])
  return (
    <AnchorWatchTile
      {...props}
      watch={atFix}
      lastUpdateAgeS={v.positionLastUpdateAgeS}
      lat={v.latitude}
      lon={v.longitude}
      depthMeters={v.depth}
      currentDriftKts={v.currentDriftKts}
      currentSetDeg={v.currentSetDeg}
      currentDriftImpactKts={v.currentDriftImpactKts}
      vesselHeadingDeg={v.headingTrue}
      aisCollisionAlarms={aisCollisionAlarms}
    />
  )
})

const POI_FIELDS = ['latitude', 'longitude', 'headingTrue', 'gnssCriticalAlert', 'positionLastUpdateAgeS'] as const

/** The Nearby map tile: own ship from vessel state, and the collision colours. */
export const LivePoiMapTile = memo(function LivePoiMapTile(props: Omit<ComponentProps<typeof PoiMapTile>,
  'aisCollisionAlarms' | (typeof POI_FIELDS)[number]>) {
  const ownShip = useVesselFields(POI_FIELDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <PoiMapTile {...props} {...ownShip} aisCollisionAlarms={aisCollisionAlarms} />
})

const ANCHOR_DRAWER_FIELDS = [
  'latitude', 'longitude', 'gnssCriticalAlert', 'headingTrue', 'depth', 'depthLastUpdateAgeS',
  'currentDriftKts', 'currentSetDeg', 'currentDriftImpactKts', 'windSpeedApparentKts', 'maxGustKts',
  'vesselLengthOverallM', 'vesselDraftM',
] as const

/**
 * The anchor watch page. Like the tile, it takes the watch record and applies
 * the live fix; everything from vessel state is filled in here.
 *
 * vesselLat/vesselLon fall back from the live fix to the anchor point (e.g.
 * GPS lost after the anchor was already set), and stay null only when neither
 * is available: the drawer renders an explicit "No GPS fix" placeholder in the
 * map slot for that case rather than being handed a fabricated 0,0.
 *
 * hasGpsFix tells the drawer whether that substitution happened. Its own
 * Adjust-mode "Alarm would sound now" check must not evaluate against the
 * anchor point (distance ~0, warning wrongly never fires) or the backend's
 * -1/-1 "no fix" sentinel (thousands of km away, warning wrongly always fires;
 * see gnss_critical_alert and ~/papercuts.md's entry on this sentinel). The
 * latitude and longitude here are a plain range check away from being -1, not
 * null, when the sentinel fires, which is why the pair is checked explicitly.
 */
export const LiveAnchorWatchDrawer = memo(function LiveAnchorWatchDrawer({ watch, ...props }: Omit<AnchorWatchDrawerProps,
  'aisCollisionAlarms' | 'vesselLat' | 'vesselLon' | 'hasGpsFix' | 'vesselHeadingDeg' | 'depthMeters' | 'depthLastUpdateAgeS'
  | 'currentDriftKts' | 'currentSetDeg' | 'currentDriftImpactKts' | 'distanceMeters' | 'bearingDeg' | 'anchorState'
  | 'canDrop' | 'windSpeedApparentKts' | 'maxGustKts' | 'vesselLengthOverallM' | 'vesselDraftM'> & { watch: AnchorWatchResult }) {
  const v = useVesselFields(ANCHOR_DRAWER_FIELDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useMemo(() => anchorWatchAt(watch, v.latitude, v.longitude, v.gnssCriticalAlert), [watch, v.latitude, v.longitude, v.gnssCriticalAlert])
  const hasFix = v.latitude !== null && v.longitude !== null
  return (
    <AnchorWatchDrawer
      {...props}
      vesselLat={v.latitude ?? watch.anchorLat}
      vesselLon={v.longitude ?? watch.anchorLon}
      hasGpsFix={hasFix && !v.gnssCriticalAlert && !(v.latitude === -1 && v.longitude === -1)}
      vesselHeadingDeg={v.headingTrue}
      depthMeters={v.depth}
      depthLastUpdateAgeS={v.depthLastUpdateAgeS}
      currentDriftKts={v.currentDriftKts}
      currentSetDeg={v.currentSetDeg}
      currentDriftImpactKts={v.currentDriftImpactKts}
      distanceMeters={atFix.distanceMeters}
      bearingDeg={atFix.bearingDeg}
      anchorState={atFix.anchorState}
      canDrop={hasFix}
      windSpeedApparentKts={v.windSpeedApparentKts}
      maxGustKts={v.maxGustKts}
      vesselLengthOverallM={v.vesselLengthOverallM}
      vesselDraftM={v.vesselDraftM}
      aisCollisionAlarms={aisCollisionAlarms}
    />
  )
})

const RADAR_FIELDS = ['latitude', 'longitude'] as const

/** The radar page, centred on the fix. */
export const LiveRadarDrawer = memo(function LiveRadarDrawer() {
  const { latitude, longitude } = useVesselFields(RADAR_FIELDS)
  return <RadarDrawer latitude={latitude} longitude={longitude} />
})

const selectLongitude = (state: VesselStateValue) => state.longitude

/** The Forecast page, which compares the boat's clock with a planned stay's. */
export function LiveForecastPage(props: Omit<ComponentProps<typeof ForecastPage>, 'boatLongitude'>) {
  const boatLongitude = useVesselStateSelector(selectLongitude)
  return <ForecastPage {...props} boatLongitude={boatLongitude} />
}

const selectSource = (state: VesselStateValue) => state.source

/** Offers to look for a SignalK server that has moved, from which source the server answered. */
export const LiveSignalKDiscoveryPrompt = memo(function LiveSignalKDiscoveryPrompt() {
  const vesselStateSource = useVesselStateSelector(selectSource)
  return <SignalKDiscoveryPrompt vesselStateSource={vesselStateSource} />
})
