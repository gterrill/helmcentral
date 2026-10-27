import { apiRequest } from "@/lib/api-query";

// Finding a place by name for the planned stay. The same place-names search
// Mate uses, centred on the boat, so the candidates come with how far away
// they are and which way.

export interface PlaceCandidate {
  name: string;
  kind: string;
  lat: number;
  lon: number;
  /** Null when the server gave none; never read as 0. */
  distanceNm: number | null;
  bearingDeg: number | null;
}

interface PlaceCandidateApi {
  name?: string;
  kind?: string;
  lat?: number;
  lon?: number;
  straight_line_nm?: number;
  straight_line_bearing_deg?: number;
}

interface PlaceSearchApi {
  results?: PlaceCandidateApi[];
}

export interface PlaceSearchResult {
  candidates: PlaceCandidate[];
}

export async function searchPlaces(query: string): Promise<PlaceSearchResult> {
  const body = await apiRequest<PlaceSearchApi>(
    `/api/place-search?q=${encodeURIComponent(query.trim())}`,
  );
  const candidates = (Array.isArray(body.results) ? body.results : [])
    .filter(
      (c) =>
        typeof c.name === "string" &&
        typeof c.lat === "number" &&
        typeof c.lon === "number",
    )
    .map((c) => ({
      name: c.name as string,
      kind: typeof c.kind === "string" ? c.kind : "",
      lat: c.lat as number,
      lon: c.lon as number,
      distanceNm: typeof c.straight_line_nm === "number" ? c.straight_line_nm : null,
      bearingDeg: typeof c.straight_line_bearing_deg === "number" ? c.straight_line_bearing_deg : null,
    }));
  // The result's own note is addressed to Mate, so it is not passed on.
  return { candidates };
}
