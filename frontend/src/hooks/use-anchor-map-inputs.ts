import { useMemo } from 'react'

import { useAnchorPlacemarks } from '@/hooks/use-anchor-placemarks'
import { anchorWatchAt, type AnchorWatchRecord, type AnchorWatchResult } from '@/hooks/use-anchor-watch'
import { useNearbyVesselList } from '@/hooks/use-nearby-vessels'
import { useRadarTargets } from '@/hooks/use-radar-targets'

/** The AIS and radar inputs both anchor maps draw. */
export function useMapContacts() {
  const aisVessels = useNearbyVesselList()
  const { targets: radarTargets, radars, source: radarSource } = useRadarTargets()
  // Undefined until the first radar event; the map reads that as not heard yet and claims nothing.
  return { aisVessels, radarTargets, radars, radarSource: radarSource ?? undefined }
}

/**
 * The watch at the live fix, keeping its object while what it says is the
 * same: distance, bearing, drag state and the GNSS flag. Keyed on those
 * outputs, not on the raw fix, so a fix that moves with no watch set (both
 * distance and bearing stay null) hands the maps below the same watch.
 */
export function useWatchAtFix(watch: AnchorWatchRecord, latitude: number | null, longitude: number | null, gnssCritical: boolean): AnchorWatchResult {
  const { distanceMeters, bearingDeg, anchorState } = anchorWatchAt(watch, latitude, longitude, gnssCritical)
  return useMemo(
    () => ({ ...watch, distanceMeters, bearingDeg, anchorState, gnssCritical }),
    [watch, distanceMeters, bearingDeg, anchorState, gnssCritical],
  )
}

/**
 * The session's pins, polled only while a watch is set: with none there is no
 * session and the server holds no pins. The tile and the page share one query.
 */
export function usePins(active: boolean) {
  const { placemarks, createPlacemark, removePlacemark } = useAnchorPlacemarks(active)
  return { placemarks, onPlacemarkCreate: createPlacemark, onPlacemarkRemove: removePlacemark }
}
