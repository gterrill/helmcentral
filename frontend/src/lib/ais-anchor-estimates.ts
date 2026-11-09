import { circlePolygon } from '@/lib/geo'

/** One nearby boat's estimated anchor, as GET /api/anchor-watch/ais-anchors sends it (ADR 0275). */
export interface AisAnchorEstimate {
  mmsi: string
  lat: number
  lon: number
  /** The fitted swing radius. */
  swing_radius_m: number
  /** The radius to draw: the fit times the proportion of room we leave round our own swing (the answer's margin_ratio). The server is the one source. */
  drawn_radius_m: number
  swing_arc_deg: number
  points: number
  /** The server's verdict: this drawn circle shares ground with the watch's own. */
  overlaps_ours: boolean
}

/** One filled circle per estimate at its drawn radius round its anchor. `overlaps` lets the layers colour the ones that reach ours. */
export function aisAnchorSwingCircles(
  estimates: readonly AisAnchorEstimate[],
): GeoJSON.FeatureCollection<GeoJSON.Polygon, { mmsi: string; overlaps: boolean }> {
  return {
    type: 'FeatureCollection',
    features: estimates.map(e => ({
      type: 'Feature',
      geometry: { type: 'Polygon', coordinates: [circlePolygon(e.lat, e.lon, e.drawn_radius_m)] },
      properties: { mmsi: e.mmsi, overlaps: e.overlaps_ours },
    })),
  }
}
