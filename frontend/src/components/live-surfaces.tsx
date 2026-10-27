import { lazy, memo, useCallback, useMemo, type ComponentProps } from 'react'

import { AlarmBanner } from '@/components/alarm-banner'
import { AlternatorTile } from '@/components/alternator-tile'
import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { AutopilotTile } from '@/components/autopilot-tile'
import { BatteryPowerTile } from '@/components/battery-power-tile'
import { ClockTile } from '@/components/clock-tile'
import { CurrentConditionsTile } from '@/components/current-conditions-tile'
import { DepthTideTile } from '@/components/depth-tide-tile'
import { DisplayShell } from '@/components/display-shell'
import { GeneratorTile } from '@/components/generator-tile'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { PoiMapTile } from '@/components/poi-map-tile'
import { PositionTile } from '@/components/position-tile'
import { RadarTargetsTile } from '@/components/radar-targets-tile'
import { RouteTile } from '@/components/route-tile'
import { SignalKDiscoveryPrompt } from '@/components/signalk-discovery-prompt'
import { SolarTile } from '@/components/solar-tile'
import { TanksTile } from '@/components/tanks-tile'
import { WindTile } from '@/components/wind-tile'
import { PLACE_NAME_REFRESH_SECONDS } from '@/config/app-config'
import { useAlarmRules } from '@/hooks/use-alarm-rules'
import { acknowledgeAlarm, silenceAlarm, useAisCollisionAlarms, useAlarmList, useKnownAlarmList } from '@/hooks/use-alarms'
import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { anchorWatchAt, useAnchorWatch, type AnchorWatchRecord, type AnchorWatchResult } from '@/hooks/use-anchor-watch'
import { useAutopilot } from '@/hooks/use-autopilot'
import { useElectricalFields } from '@/hooks/use-electrical-state'
import { useNearbyVesselList, useNearbyVessels } from '@/hooks/use-nearby-vessels'
import { usePlaceName } from '@/hooks/use-place-name'
import { useRadarTargets } from '@/hooks/use-radar-targets'
import { useServerTrails } from '@/hooks/use-server-trails'
import { useSocBands } from '@/hooks/use-soc-bands'
import { useSolarState } from '@/hooks/use-solar-state'
import { useTanksState } from '@/hooks/use-tanks-state'
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

/** The Alarms page: the whole active list, or null until it is known, and the two actions. */
export const LiveAlarmsDrawer = memo(function LiveAlarmsDrawer(props: Omit<AlarmsDrawerProps, 'alarms' | 'onAcknowledge' | 'onSilence'>) {
  const alarms = useKnownAlarmList()
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

/** The Nearby Vessels list: the contacts from their own stream, and the collision highlight on each row. */
export const LiveNearbyVesselsTile = memo(function LiveNearbyVesselsTile(props: Omit<ComponentProps<typeof NearbyVesselsTile>,
  'aisCollisionAlarms' | 'vessels' | 'loading' | 'lastUpdateAgeS' | 'maxRangeM'>) {
  const nearby = useNearbyVessels()
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <NearbyVesselsTile {...props} {...nearby} aisCollisionAlarms={aisCollisionAlarms} />
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
  const placeName = usePlaceName(fix.latitude !== null && fix.longitude !== null, PLACE_NAME_REFRESH_SECONDS)
  return <PositionTile lastUpdateAgeS={positionLastUpdateAgeS} {...fix} placeName={placeName} />
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
 * The clock tile: the place name, and the trip ETA (ADR 0092, ADR 0125).
 * computeClockTripEta reports to the final waypoint of the route SignalK is
 * navigating, or to the Course API's bare next point when a chartplotter
 * go-to has no route behind it.
 *
 * The ETA needs the fix and speed only while there is a trip. Without one the
 * tile reads just whether there is a fix, which is all the place name asks,
 * so a clock on a boat at anchor does not redraw on every fix.
 */
export const LiveClockTile = memo(function LiveClockTile({ routeStatus, ...props }: Omit<ComponentProps<typeof ClockTile>, 'placeName' | 'tripEta'> & { routeStatus: ActiveRouteStatus }) {
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

const GENERATOR_ELECTRICAL_FIELDS = ['generatorRealPowerW', 'generatorConfigured', 'generatorNone', 'batterySocPercent', 'batteryRatePercentPerHour'] as const

/** The Generator tile: its own state from vessel state, its power, its role and the battery from the electrical stream. */
export const LiveGeneratorTile = memo(function LiveGeneratorTile(props: Omit<ComponentProps<typeof GeneratorTile>,
  (typeof GENERATOR_FIELDS)[number] | (typeof GENERATOR_ELECTRICAL_FIELDS)[number]>) {
  const generator = useVesselFields(GENERATOR_FIELDS)
  const electrical = useElectricalFields(GENERATOR_ELECTRICAL_FIELDS)
  return <GeneratorTile {...props} {...generator} {...electrical} />
})

const selectHeading = (state: VesselStateValue) => state.headingTrue

/** The autopilot tile: the pilot's state and controls from its own stream, the heading from vessel state. */
export const LiveAutopilotTile = memo(function LiveAutopilotTile({ readOnly }: { readOnly: boolean }) {
  const autopilot = useAutopilot()
  const headingTrueDeg = useVesselStateSelector(selectHeading)
  return (
    <AutopilotTile
      state={autopilot.state}
      pending={autopilot.pending}
      error={autopilot.error}
      availableModes={autopilot.availableModes}
      capabilityError={autopilot.capabilityError}
      onEngage={autopilot.engage}
      onDisengage={autopilot.disengage}
      onTack={autopilot.tack}
      onGybe={autopilot.gybe}
      onAdjustHeading={autopilot.adjustHeading}
      onSetMode={autopilot.setMode}
      onDodge={autopilot.dodge}
      onClearDodge={autopilot.clearDodge}
      headingTrueDeg={headingTrueDeg}
      readOnly={readOnly}
    />
  )
})

const selectEnginesRunning = (state: VesselStateValue) => state.enginesRunning
const ALTERNATOR_FIELDS = ['alternator0', 'alternator1'] as const

/**
 * The alternator tile: the readings from the electrical stream, shown while an
 * engine runs. Whether one runs is null until vessel state says, and the tile
 * then claims neither.
 */
export const LiveAlternatorTile = memo(function LiveAlternatorTile() {
  const enginesRunning = useVesselStateSelector(selectEnginesRunning)
  const { alternator0, alternator1 } = useElectricalFields(ALTERNATOR_FIELDS)
  return <AlternatorTile port={alternator0} starboard={alternator1} enginesRunning={enginesRunning} />
})

const BATTERY_FIELDS = [
  'batterySocPercent', 'socPath', 'chargingCurrentA', 'chargingPowerW', 'solarOutputW', 'acOutputW', 'dc12vPowerW',
  'voltageReadoutV', 'voltageReadoutLabel', 'charger0CurrentA', 'charger0AcIn1CurrentA', 'charger0ChargingMode',
  'charger0Error', 'batteryRatePercentPerHour', 'timeToGoHours', 'lastUpdateAgeS', 'houseBankConfigured', 'houseBankUnresolved',
] as const

/**
 * Battery & Power: the electrical stream, and the SoC bands from the alarm
 * rules on the house bank's path. The rules come from the same query the Alarms
 * page edits, so a rule saved there recolours the bar without a reload.
 */
export const LiveBatteryPowerTile = memo(function LiveBatteryPowerTile(props: Pick<ComponentProps<typeof BatteryPowerTile>, 'overnight' | 'onOpenRoles'>) {
  const { socPath, ...electrical } = useElectricalFields(BATTERY_FIELDS)
  const { rules } = useAlarmRules()
  const socBands = useSocBands(rules, socPath)
  return <BatteryPowerTile {...props} {...electrical} socBands={socBands} />
})

/** The Solar tile: the whole solar stream. */
export const LiveSolarTile = memo(function LiveSolarTile() {
  return <SolarTile {...useSolarState()} />
})

/** The Tanks tile: the tanks stream. */
export const LiveTanksTile = memo(function LiveTanksTile() {
  return <TanksTile {...useTanksState()} />
})

/** The Radar Targets list: the radar stream. */
export const LiveRadarTargetsTile = memo(function LiveRadarTargetsTile(props: Pick<ComponentProps<typeof RadarTargetsTile>, 'distanceUnits'>) {
  return <RadarTargetsTile {...props} {...useRadarTargets()} />
})

/** The AIS and radar inputs both anchor maps draw. */
function useMapContacts() {
  const aisVessels = useNearbyVesselList()
  const { targets: radarTargets, radars, source: radarSource } = useRadarTargets()
  return { aisVessels, radarTargets, radars, radarSource }
}

/** How often a map on screen asks the server for new trail points. */
const TRAIL_POLL_MS = 5000

/**
 * The watch at the live fix, keeping its object while what it says is the
 * same: distance, bearing, drag state and the GNSS flag. Keyed on those
 * outputs, not on the raw fix, so a fix that moves with no watch set (both
 * distance and bearing stay null) hands the maps below the same watch.
 */
function useWatchAtFix(watch: AnchorWatchRecord, latitude: number | null, longitude: number | null, gnssCritical: boolean): AnchorWatchResult {
  const { distanceMeters, bearingDeg, anchorState } = anchorWatchAt(watch, latitude, longitude, gnssCritical)
  return useMemo(
    () => ({ ...watch, distanceMeters, bearingDeg, anchorState, gnssCritical }),
    [watch, distanceMeters, bearingDeg, anchorState, gnssCritical],
  )
}

const ANCHOR_FIELDS = [
  'positionLastUpdateAgeS', 'latitude', 'longitude', 'gnssCriticalAlert', 'depth',
  'currentDriftKts', 'currentSetDeg', 'currentDriftImpactKts', 'headingTrue',
] as const

/**
 * The session's pins, polled only while a watch is set: with none there is no
 * session and the server holds no pins. The tile and the page share one query.
 */
function usePins(active: boolean) {
  const { placemarks, createPlacemark, removePlacemark } = useAnchorPlacemarks(active)
  return { placemarks, onPlacemarkCreate: createPlacemark, onPlacemarkRemove: removePlacemark }
}

/**
 * The anchor watch tile. It reads the watch record, applies the live fix to it
 * (distance, bearing, drag state), and adds depth, current, heading, the
 * collision colours, the pins, the trails and the AIS and radar contacts. The
 * trails poll only while the tile is on screen.
 */
export const LiveAnchorWatchTile = memo(function LiveAnchorWatchTile(props: Omit<ComponentProps<typeof AnchorWatchTile>,
  'watch' | 'aisCollisionAlarms' | 'lastUpdateAgeS' | 'lat' | 'lon' | 'depthMeters' | 'currentDriftKts' | 'currentSetDeg' | 'currentDriftImpactKts' | 'vesselHeadingDeg'
  | 'vesselTrail' | 'aisTrails' | 'placemarks' | 'onPlacemarkCreate' | 'onPlacemarkRemove'
  | 'aisVessels' | 'radarTargets' | 'radars' | 'radarSource'>) {
  const watch = useAnchorWatch()
  const contacts = useMapContacts()
  const v = useVesselFields(ANCHOR_FIELDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useWatchAtFix(watch, v.latitude, v.longitude, v.gnssCriticalAlert)
  const { getAnchorTrail } = useAnchorTrail(TRAIL_POLL_MS)
  const { getAisTrails } = useServerTrails(TRAIL_POLL_MS)
  const pins = usePins(watch.active)
  return (
    <AnchorWatchTile
      {...props}
      {...pins}
      {...contacts}
      watch={atFix}
      vesselTrail={getAnchorTrail}
      aisTrails={getAisTrails}
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

/** The Nearby map tile: own ship from vessel state, the AIS contacts, the collision colours, and the boat's trail while it is on screen. */
export const LivePoiMapTile = memo(function LivePoiMapTile(props: Omit<ComponentProps<typeof PoiMapTile>,
  'aisCollisionAlarms' | 'getSelfTrail' | 'nearbyVessels' | (typeof POI_FIELDS)[number]>) {
  const ownShip = useVesselFields(POI_FIELDS)
  const nearbyVessels = useNearbyVesselList()
  const aisCollisionAlarms = useAisCollisionAlarms()
  const { getSelfTrail } = useServerTrails(TRAIL_POLL_MS)
  return <PoiMapTile {...props} {...ownShip} nearbyVessels={nearbyVessels} aisCollisionAlarms={aisCollisionAlarms} getSelfTrail={getSelfTrail} />
})

const ANCHOR_DRAWER_FIELDS = [
  'latitude', 'longitude', 'gnssCriticalAlert', 'headingTrue', 'depth', 'depthLastUpdateAgeS',
  'currentDriftKts', 'currentSetDeg', 'currentDriftImpactKts', 'windSpeedApparentKts', 'maxGustKts',
  'vesselLengthOverallM', 'vesselDraftM',
] as const

/**
 * The anchor watch page. Like the tile, it reads the watch record and applies
 * the live fix; the record's fields and writes, the pins, the trails, the AIS
 * and radar contacts and everything from vessel state are filled in here.
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
export const LiveAnchorWatchDrawer = memo(function LiveAnchorWatchDrawer(props: Omit<AnchorWatchDrawerProps,
  'aisCollisionAlarms' | 'vesselLat' | 'vesselLon' | 'hasGpsFix' | 'vesselHeadingDeg' | 'depthMeters' | 'depthLastUpdateAgeS'
  | 'currentDriftKts' | 'currentSetDeg' | 'currentDriftImpactKts' | 'distanceMeters' | 'bearingDeg' | 'anchorState'
  | 'canDrop' | 'windSpeedApparentKts' | 'maxGustKts' | 'vesselLengthOverallM' | 'vesselDraftM'
  | 'anchorLat' | 'anchorLon' | 'radiusMeters' | 'bowOffsetM' | 'bowOffsetApplied' | 'bowOffsetReason' | 'headingAtSetDeg'
  | 'placeName' | 'anchorSetAt' | 'anchorStateKnown' | 'error' | 'onRadiusChange' | 'adjustAnchor' | 'onClearAnchor'
  | 'rodeDeployedM' | 'seaState' | 'seabedType' | 'onUpdateRodeAndConditions'
  | 'vesselTrail' | 'aisTrails' | 'placemarks' | 'onPlacemarkCreate' | 'onPlacemarkRemove'
  | 'aisVessels' | 'radarTargets' | 'radars' | 'radarSource'>) {
  const watch = useAnchorWatch()
  const contacts = useMapContacts()
  const v = useVesselFields(ANCHOR_DRAWER_FIELDS)
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useWatchAtFix(watch, v.latitude, v.longitude, v.gnssCriticalAlert)
  const { getAnchorTrail } = useAnchorTrail(TRAIL_POLL_MS)
  const { getAisTrails } = useServerTrails(TRAIL_POLL_MS)
  const pins = usePins(watch.active)
  const hasFix = v.latitude !== null && v.longitude !== null
  return (
    <AnchorWatchDrawer
      {...props}
      {...pins}
      {...contacts}
      anchorLat={watch.anchorLat}
      anchorLon={watch.anchorLon}
      radiusMeters={watch.radiusMeters}
      bowOffsetM={watch.bowOffsetM}
      bowOffsetApplied={watch.bowOffsetApplied}
      bowOffsetReason={watch.bowOffsetReason}
      headingAtSetDeg={watch.headingAtSetDeg}
      placeName={watch.placeName}
      anchorSetAt={watch.setAt}
      anchorStateKnown={watch.loaded}
      error={watch.error}
      onRadiusChange={watch.updateRadius}
      adjustAnchor={watch.adjustAnchor}
      onClearAnchor={watch.clearAnchor}
      rodeDeployedM={watch.rodeDeployedM}
      seaState={watch.seaState}
      seabedType={watch.seabedType}
      onUpdateRodeAndConditions={watch.updateRodeAndConditions}
      vesselTrail={getAnchorTrail}
      aisTrails={getAisTrails}
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
