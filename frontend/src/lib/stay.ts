import { haversineMeters } from "@/lib/geo";

// Date arithmetic for a planned stay. Arrival and departure are calendar days
// at the place (YYYY-MM-DD), compared as strings and shifted in UTC so the
// browser's own zone never moves a day.

export const TIDE_STATION_FAR_NM = 20;
const METRES_PER_NM = 1852;
const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
];

export interface PlannedStay {
  name: string;
  lat: number;
  lon: number;
  arrive: string;
  depart: string;
}

function parts(dayKey: string): [number, number, number] {
  const [year, month, day] = dayKey.split("-").map(Number);
  return [year, month, day];
}

export function addDaysISO(dayKey: string, days: number): string {
  const [year, month, day] = parts(dayKey);
  return new Date(Date.UTC(year, month - 1, day + days))
    .toISOString()
    .slice(0, 10);
}

export function daysBetweenISO(from: string, to: string): number {
  const [fy, fm, fd] = parts(from);
  const [ty, tm, td] = parts(to);
  return Math.round(
    (Date.UTC(ty, tm - 1, td) - Date.UTC(fy, fm - 1, fd)) / 86_400_000,
  );
}

/** Short weekday ("Sat") for a calendar day, read in UTC so the device's own zone never shifts it. */
export function formatWeekday(dayKey: string): string {
  const [year, month, day] = parts(dayKey);
  return new Date(Date.UTC(year, month - 1, day)).toLocaleDateString("en-US", {
    weekday: "short",
    timeZone: "UTC",
  });
}

/** "27 Oct". */
export function formatDayMonth(dayKey: string): string {
  const [, month, day] = parts(dayKey);
  return `${day} ${MONTHS[month - 1]}`;
}

/** "27–31 Oct", "30 Oct – 2 Nov", "27 Oct" for a single day. */
export function formatStayRange(arrive: string, depart: string): string {
  if (arrive === depart) return formatDayMonth(arrive);
  const [, arriveMonth, arriveDay] = parts(arrive);
  const [, departMonth] = parts(depart);
  if (arriveMonth === departMonth)
    return `${arriveDay}–${formatDayMonth(depart)}`;
  return `${formatDayMonth(arrive)} – ${formatDayMonth(depart)}`;
}

export function isStayDay(
  dayKey: string,
  stay: Pick<PlannedStay, "arrive" | "depart">,
): boolean {
  return dayKey >= stay.arrive && dayKey <= stay.depart;
}

export interface PendingStayDay {
  dayKey: string;
  /** The day the forecast first reaches this one: when it becomes the last day of the window. */
  availableFrom: string;
}

/**
 * Stay days the forecast does not reach yet. The window is as long as the
 * forecast the server returned, so a day D joins it once D is its last day.
 * Nothing is estimated for these: they are listed so the stay is not
 * mistaken for covered.
 */
export function pendingStayDays(
  forecastDayKeys: string[],
  stay: Pick<PlannedStay, "arrive" | "depart">,
): PendingStayDay[] {
  if (forecastDayKeys.length === 0) return [];
  const last = forecastDayKeys[forecastDayKeys.length - 1];
  const windowDays = forecastDayKeys.length;
  const pending: PendingStayDay[] = [];
  for (
    let day = addDaysISO(last, 1);
    day <= stay.depart;
    day = addDaysISO(day, 1)
  ) {
    if (day < stay.arrive) continue;
    pending.push({
      dayKey: day,
      availableFrom: addDaysISO(day, -(windowDays - 1)),
    });
  }
  return pending;
}

export function distanceNm(
  lat1: number,
  lon1: number,
  lat2: number,
  lon2: number,
): number {
  return haversineMeters(lat1, lon1, lat2, lon2) / METRES_PER_NM;
}

export function isFarFromPlace(nm: number): boolean {
  return nm > TIDE_STATION_FAR_NM;
}
