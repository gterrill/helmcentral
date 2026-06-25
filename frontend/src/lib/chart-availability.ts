/**
 * STUB: real S-57/ENC chart-coverage detection does not exist yet in this
 * repo (no GDAL, no ENC parsing, no chart-coverage catalog). Until that
 * lands, this is the explicit, hardcoded placeholder gating the GSHHG
 * coastline fallback in RoutePlannerMap — it always reports "no chart
 * available" so the fallback always renders. Replace the body of this
 * function with a real coverage check (e.g. a lookup against a
 * chart-coverage catalog keyed by the current map viewport) when S-57
 * ingestion exists. See docs/adr/0009-gshhg-coastline-fallback.md.
 */
export function isChartAvailable(): boolean {
  return false
}
