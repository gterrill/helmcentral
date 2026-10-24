// A forecast feed asked about a named place (the planned stay) instead of the
// boat. Without a place the key and the path are exactly what the dashboard
// has always used, so its queries are untouched; with one they get their own
// key per position, so the stay view never shares a cache entry with the
// own-ship feed that drives the tiles.

export interface ForecastPlace {
  lat: number;
  lon: number;
}

export interface ForecastQueryOptions {
  place?: ForecastPlace;
  /** False holds the query back entirely (the stay view is not open). */
  enabled?: boolean;
}

export function forecastQuery(
  path: string,
  place: ForecastPlace | undefined,
): { queryKey: readonly unknown[]; path: string } {
  if (!place) return { queryKey: [path], path };
  const params = new URLSearchParams({
    lat: String(place.lat),
    lon: String(place.lon),
  });
  return {
    queryKey: [path, { lat: place.lat, lon: place.lon }],
    path: `${path}?${params.toString()}`,
  };
}
