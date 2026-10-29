import { lazy, memo, useCallback, useMemo, type ComponentProps } from 'react'

import { FORECAST_REFRESH_SECONDS, TRAIL_POLL_MS } from '@/config/app-config'
import { useAlarmRules } from '@/hooks/use-alarm-rules'
import { acknowledgeAlarm, silenceAlarm, useAisCollisionAlarms, useKnownAlarmList } from '@/hooks/use-alarms'
import { setAnchorHere, useAnchorWatch } from '@/hooks/use-anchor-watch'
import { useAnchorTrail } from '@/hooks/use-anchor-trail'
import { useForecastWarnings } from '@/hooks/use-forecast-warnings'
import { useAisTrails } from '@/hooks/use-server-trails'
import { useTideToday } from '@/hooks/use-tide-today'
import { useUpperAir } from '@/hooks/use-upper-air'
import { selectTimeZone } from '@/lib/vessel-time'
import { getVesselStateSnapshot, useVesselFields, useVesselStateSelector } from '@/hooks/use-vessel-state'
import { useWaveForecast } from '@/hooks/use-wave-forecast'
import { useWeatherForecast } from '@/hooks/use-weather-forecast'
import { useApiQuerySelector } from '@/lib/api-query'
import { collisionTuningUrl as buildCollisionTuningUrl } from '@/lib/collision-tuning'
import { buildForecastDrawerProps } from '@/lib/forecast-drawer-props'
import { tideHeightFtOrNull } from '@/lib/rode-plan'
import { useMapContacts, usePins, useWatchAtFix } from '@/hooks/use-anchor-map-inputs'

/*
 * The wrappers for the pages AppShell opens as lazy chunks (ADR 0215): Alarms,
 * Anchor Watch, Radar and Forecast. They cannot sit beside the page they feed,
 * because the wrapper is eager and the page is not: a wrapper in the page's
 * own file would pull its chunk into the main bundle. So the pages stay behind
 * React.lazy here, as in App.tsx's own panels, and each wrapper reads its
 * stores and hands the page the props it always took.
 */

// The drawers are lazy in App.tsx too; these resolve to the same chunks.
const AlarmsDrawer = lazy(() => import('@/components/alarms-drawer').then((mod) => ({ default: mod.AlarmsDrawer })))
const AnchorWatchDrawer = lazy(() => import('@/components/anchor-watch-drawer').then((mod) => ({ default: mod.AnchorWatchDrawer })))
const ForecastPage = lazy(() => import('@/components/forecast-page').then((mod) => ({ default: mod.ForecastPage })))
const RadarDrawer = lazy(() => import('@/components/radar-drawer').then((mod) => ({ default: mod.RadarDrawer })))

type AlarmsDrawerProps = ComponentProps<typeof AlarmsDrawer>
type AnchorWatchDrawerProps = ComponentProps<typeof AnchorWatchDrawer>

const selectCollisionTuningUrl = (settings: { signalk?: { address?: string; port?: number } } | undefined) =>
  buildCollisionTuningUrl(settings?.signalk?.address, settings?.signalk?.port)

/**
 * The Alarms page: the whole active list, or null until it is known, and the
 * two actions; the rules it edits; the forecast warnings its forecast cards
 * link to; and the AIS Target Prioritizer link, which lives on the SignalK
 * server and so is only as good as the configured address (ADR 0090). With no
 * address, or settings that could not be read, there is no link.
 */
export const LiveAlarmsDrawer = memo(function LiveAlarmsDrawer(props: Omit<AlarmsDrawerProps,
  'alarms' | 'onAcknowledge' | 'onSilence' | 'rules' | 'loading' | 'error' | 'createRule' | 'updateRule' | 'deleteRule'
  | 'collisionTuningUrl' | 'forecastWarnings'>) {
  const alarms = useKnownAlarmList()
  const { rules, loading, error, createRule, updateRule, deleteRule } = useAlarmRules()
  const { activeWarning } = useForecastWarnings(FORECAST_REFRESH_SECONDS)
  const collisionTuningUrl = useApiQuerySelector({ queryKey: ['/api/settings'], path: '/api/settings' }, selectCollisionTuningUrl)
  return (
    <AlarmsDrawer
      {...props}
      alarms={alarms}
      onAcknowledge={acknowledgeAlarm}
      onSilence={silenceAlarm}
      rules={rules}
      loading={loading}
      error={error}
      createRule={createRule}
      updateRule={updateRule}
      deleteRule={deleteRule}
      collisionTuningUrl={collisionTuningUrl}
      forecastWarnings={activeWarning}
    />
  )
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
  | 'aisVessels' | 'radarTargets' | 'radars' | 'radarSource' | 'tide' | 'onDropAnchor'>) {
  const watch = useAnchorWatch()
  const contacts = useMapContacts()
  const v = useVesselFields(ANCHOR_DRAWER_FIELDS)
  const { tide } = useTideToday(FORECAST_REFRESH_SECONDS)
  // Read at the tap, not subscribed: the fix and depth come from the store as
  // the operator drops, and the tide from the reading this page shows.
  const onDropAnchor = useCallback(() => {
    const { latitude, longitude, depth } = getVesselStateSnapshot()
    if (latitude === null || longitude === null) return
    void setAnchorHere(latitude, longitude, {
      planningDepthM: depth,
      planningTideHeightFt: tideHeightFtOrNull(tide),
    })
  }, [tide])
  const aisCollisionAlarms = useAisCollisionAlarms()
  const atFix = useWatchAtFix(watch, v.latitude, v.longitude, v.gnssCriticalAlert)
  const anchorTrail = useAnchorTrail(TRAIL_POLL_MS)
  const aisTrails = useAisTrails(TRAIL_POLL_MS)
  const pins = usePins(watch.active)
  const hasFix = v.latitude !== null && v.longitude !== null
  return (
    <AnchorWatchDrawer
      {...props}
      {...pins}
      {...contacts}
      tide={tide}
      onDropAnchor={onDropAnchor}
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
      vesselTrail={anchorTrail}
      aisTrails={aisTrails}
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


/**
 * The Forecast page: the boat's own forecast feeds, and the boat's time zone it
 * compares a planned stay's clock with. The page fetches a planned stay's feeds
 * itself.
 */
export const LiveForecastPage = memo(function LiveForecastPage({ unit, ...props }: Omit<ComponentProps<typeof ForecastPage>, 'boatTimeZone' | 'boat'> & { unit: 'imperial' | 'metric' }) {
  const boatTimeZone = useVesselStateSelector(selectTimeZone)
  const weather = useWeatherForecast(FORECAST_REFRESH_SECONDS)
  const waves = useWaveForecast(FORECAST_REFRESH_SECONDS)
  // Upper air keeps its own cadence: the global models behind it run four
  // times a day, so the surface-forecast interval would fetch the same numbers.
  const upperAir = useUpperAir()
  const warnings = useForecastWarnings(FORECAST_REFRESH_SECONDS)
  const boat = useMemo(
    () => buildForecastDrawerProps({ weather, waves, upperAir, warnings, unit }),
    [weather, waves, upperAir, warnings, unit],
  )
  return <ForecastPage {...props} boat={boat} boatTimeZone={boatTimeZone} />
})
