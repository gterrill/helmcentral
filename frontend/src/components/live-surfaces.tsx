import { lazy, memo, type ComponentProps } from 'react'

import { AlarmBanner } from '@/components/alarm-banner'
import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import { DisplayShell } from '@/components/display-shell'
import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { PoiMapTile } from '@/components/poi-map-tile'
import { acknowledgeAlarm, silenceAlarm, useAisCollisionAlarms, useAlarmList } from '@/hooks/use-alarms'

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

/** The anchor watch tile, for the collision colour on its AIS markers. */
export const LiveAnchorWatchTile = memo(function LiveAnchorWatchTile(props: Omit<ComponentProps<typeof AnchorWatchTile>, 'aisCollisionAlarms'>) {
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <AnchorWatchTile {...props} aisCollisionAlarms={aisCollisionAlarms} />
})

/** The Nearby map tile, for the collision colour on its AIS markers. */
export const LivePoiMapTile = memo(function LivePoiMapTile(props: Omit<ComponentProps<typeof PoiMapTile>, 'aisCollisionAlarms'>) {
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <PoiMapTile {...props} aisCollisionAlarms={aisCollisionAlarms} />
})

/** The anchor watch page, for the collision colour on its AIS markers. */
export const LiveAnchorWatchDrawer = memo(function LiveAnchorWatchDrawer(props: Omit<AnchorWatchDrawerProps, 'aisCollisionAlarms'>) {
  const aisCollisionAlarms = useAisCollisionAlarms()
  return <AnchorWatchDrawer {...props} aisCollisionAlarms={aisCollisionAlarms} />
})
