import { useApiQuery } from "@/lib/api-query";
import type { TideStation } from "@/hooks/use-tide-stations";
import { distanceNm } from "@/lib/stay";

// The tide station nearest a named place, for the stay view only. It is a
// read: the operator's saved station and the auto-station setting are never
// touched.

interface NearestApi {
  station_id?: string;
  name?: string;
  state?: string;
  lat?: number;
  lon?: number;
  timezone?: string;
}

export interface PlaceTideStation {
  station: Pick<TideStation, "stationId" | "name" | "state" | "timezone">;
  /** Null when the station's position is unknown; never measured against 0,0. */
  distanceNm: number | null;
}

export function usePlaceTideStation(
  provider: string,
  place: { lat: number; lon: number } | null,
) {
  const params = new URLSearchParams({ provider });
  if (place) {
    params.set("lat", String(place.lat));
    params.set("lon", String(place.lon));
  }
  const { data, error, isLoading } = useApiQuery<PlaceTideStation>({
    queryKey: [
      "/api/tide-nearest",
      { provider, lat: place?.lat, lon: place?.lon },
    ],
    path: `/api/tide-nearest?${params.toString()}`,
    enabled: !!provider && !!place,
    select: (raw: NearestApi) => {
      if (!raw.station_id || !place)
        throw new Error("No tide station near this place");
      const station = {
        stationId: raw.station_id,
        name: raw.name ?? "",
        state: raw.state,
        timezone: raw.timezone,
      };
      const located =
        typeof raw.lat === "number" && typeof raw.lon === "number";
      return {
        station,
        distanceNm: located
          ? distanceNm(
              place.lat,
              place.lon,
              raw.lat as number,
              raw.lon as number,
            )
          : null,
      };
    },
  });
  return {
    nearest: data ?? null,
    loading: isLoading,
    error: error
      ? error.message || "No tide station found near this place"
      : null,
  };
}
