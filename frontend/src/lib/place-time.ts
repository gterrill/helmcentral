// Local time at a place the boat is not at. The server buckets a forecast into
// days by a fixed whole-hour offset worked out from the place's longitude
// (backend vesselLocalLocation); this is the same arithmetic, so a day the
// server calls Tuesday starts and ends here at the same instants.

import { vesselZone } from "@/lib/passages";

/** Whole-hour UTC offset for a longitude, as the server reckons it. */
export function placeUtcOffsetHours(lon: number): number {
  return vesselZone(lon).offsetHours;
}

/** IANA zone name carrying that offset. POSIX Etc/GMT zones invert the sign. */
export function placeTimeZoneName(lon: number): string {
  const hours = placeUtcOffsetHours(lon);
  if (hours === 0) return "UTC";
  return `Etc/GMT${hours > 0 ? "-" : "+"}${Math.abs(hours)}`;
}

/** "UTC+10", "UTC-5", "UTC". */
export function placeOffsetLabel(lon: number): string {
  const hours = placeUtcOffsetHours(lon);
  if (hours === 0) return "UTC";
  return `UTC${hours > 0 ? "+" : "-"}${Math.abs(hours)}`;
}

/** True when the place keeps a different clock from the boat's. */
export function placeClockDiffers(
  placeLon: number,
  boatLon: number | null,
): boolean {
  if (boatLon === null || !Number.isFinite(boatLon)) return false;
  return placeUtcOffsetHours(placeLon) !== placeUtcOffsetHours(boatLon);
}

/** The instant a calendar day (YYYY-MM-DD) begins at the place. */
export function placeDayStart(dayKey: string, lon: number): Date {
  const [year, month, day] = dayKey.split("-").map(Number);
  return new Date(
    Date.UTC(year, month - 1, day) - placeUtcOffsetHours(lon) * 3_600_000,
  );
}
