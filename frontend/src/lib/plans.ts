import { circularMeanDegOrNull } from "@/lib/boat-model";
import { shortTurn } from "@/lib/replay-camera";
import { addDaysISO, daysBetweenISO } from "@/lib/stay";

// Passage plans: the operator's intended run, stop by stop. The server owns
// the date rule (every arrival after the first is the stop before's arrival
// plus its nights); this file lays a plan out as one row per day and reads a
// day's forecast against a leg's course.

export interface PlanVia {
  name: string;
  lat: number;
  lon: number;
}

export interface PlanStop {
  name: string;
  lat: number;
  lon: number;
  /** Calendar day at the place, YYYY-MM-DD. */
  arrive: string;
  nights: number;
  /** Day the boat leaves: arrive plus nights. */
  depart: string;
  /** Places the leg leaving this stop passes, in order. Empty on the last stop. */
  vias: PlanVia[];
  /** Operator's departure time for the leg leaving this stop, HH:MM; "" when none. */
  departTime: string;
  /** IANA zone at the stop; the server works it out. */
  timezone: string;
}

export type PlanStatus = "upcoming" | "active" | "past";

export interface Plan {
  id: number;
  name: string;
  stops: PlanStop[];
  status: PlanStatus;
  firstDate: string;
  lastDate: string;
}

/**
 * What the operator sends: the server adds the id, status, departures and
 * zones. Only the first stop's arrival is sent; the server dates the rest.
 */
export interface PlanInput {
  name: string;
  stops: Array<
    Pick<PlanStop, "name" | "lat" | "lon" | "nights" | "vias" | "departTime"> & { arrive?: string }
  >;
}

export interface PlanLeg {
  from: number;
  to: number;
  /** Null when the leg could not be routed; `error` says why. */
  distanceNm: number | null;
  courseDeg: number | null;
  route: Array<[number, number]>;
  note: string;
  /** Why the leg failed, in the router's words: the tooltip's full sentence. */
  error: string;
  /** The router's words alone, without the "no passage distance for A to B" lead. */
  reason: string;
  /** "on_land" when a stop is off the water, "closed_off" when the shoreline cuts two places apart; empty for any other failure. */
  errorKind: string;
  errorStop: string;
  /** With "closed_off", the place after errorStop. */
  errorTo: string;
  /** Stops charted behind the shoreline and how far, in nautical miles. */
  moved: Array<{ stop: string; nm: number }>;
}

export type LegVerdict = "go" | "marginal" | "no_go" | "no_data";

/**
 * What sort of reason a leg's note is, sent by the server so the table never
 * reads the words. "problem" is the one thing said above the table for the
 * whole plan; "no_forecast" is a day past the forecast; "" is no note.
 */
export type PlanNoteKind =
  | ""
  | "problem"
  | "no_forecast"
  | "not_routed"
  | "past"
  | "overnight"
  | "no_window"
  | "window_passed"
  | "no_departure"
  | "gap"
  | "other";

/**
 * A leg's verdict and the figures beside it, from the server's assessment
 * (ADR 0286). `verdict` is null when the leg has none and `note` says why.
 * `depart` and `arrive` are instants; `departSuggested` marks a departure the
 * operator did not set. `currentKn` is positive when the current helps the boat,
 * null when it could not be read.
 */
export interface PlanLegAssessment {
  leg: number;
  date: string;
  verdict: LegVerdict | null;
  limiting: string;
  note: string;
  noteKind: PlanNoteKind;
  depart: string | null;
  departSuggested: boolean;
  arrive: string | null;
  hours: number | null;
  speedKn: number | null;
  currentKn: number | null;
  currentNote: string;
  notChecked: string[];
}

/** An official warning in force the night the boat is at a stop. */
export interface PlanStopWarning {
  date: string;
  warning: string;
  url: string;
}

export interface PlanAssessment {
  /** When the verdicts were worked out. */
  asOf: string;
  /** When the oldest forecast they read was fetched; null when none was read. */
  forecastAsOf: string | null;
  /** The one thing that stopped the verdicts for the whole plan. */
  problem: string;
  /** Why warnings could not be read; empty when they could. */
  warningsError: string;
  legs: PlanLegAssessment[];
  stops: Array<{ stop: number; nights: PlanStopWarning[] }>;
}

/**
 * The verdict column's text: "Go", "Marginal: wind 15 kn ahead", "No-Go: gusts
 * 32 kn ahead", "No data". A leg with no verdict says why instead; null when it
 * has neither.
 */
export function verdictText(leg: Pick<PlanLegAssessment, "verdict" | "limiting" | "note">): string | null {
  switch (leg.verdict) {
    case "go":
      return "Go";
    case "marginal":
      return leg.limiting ? `Marginal: ${leg.limiting}` : "Marginal";
    case "no_go":
      return leg.limiting ? `No-Go: ${leg.limiting}` : "No-Go";
    case "no_data":
      return "No data";
    default:
      return leg.note === "" ? null : leg.note;
  }
}

/** "0.8 kn hindering", "1.0 kn helping", "0.0 kn" when it rounds to nothing. */
export function currentText(kn: number): string {
  const size = Math.abs(kn).toFixed(1);
  if (size === "0.0") return `${size} kn`;
  return `${size} kn ${kn > 0 ? "helping" : "hindering"}`;
}

/** The operator's wording for a failed leg; `detail` is the router's message. */
export function legErrorText(leg: Pick<PlanLeg, "error" | "reason" | "errorKind" | "errorStop" | "errorTo">): {
  summary: string;
  detail: string;
} {
  if (leg.errorKind === "closed_off" && leg.errorStop && leg.errorTo) {
    return {
      summary: `Can't route: the water between ${leg.errorStop} and ${leg.errorTo} is closed off on the chart. Move a stop or add a via place.`,
      detail: leg.error,
    };
  }
  if (leg.errorKind === "on_land" && leg.errorStop) {
    return {
      summary: `Can't route: ${leg.errorStop} is on land here. Change the stop to a spot on the water.`,
      detail: leg.error,
    };
  }
  return { summary: `Can't route this leg: ${leg.reason}`, detail: leg.error };
}

/** The operator's wording for stops charted behind the shoreline. */
export function legNoteText(leg: Pick<PlanLeg, "moved">): string {
  return leg.moved
    .map((m) => `${m.stop} is ${m.nm.toFixed(1)} nm inside the charted shoreline; the distance starts at the nearest water.`)
    .join(" ");
}

/**
 * `key` is what the row is, not its date: "stop-2-1" is stop 2's second lay
 * day, "leg-0" the passage leaving stop 0. It holds when a night added earlier
 * moves every later date, so a selection can be kept by key.
 */
export type PlanRow =
  | { kind: "stop"; key: string; dayKey: string; dayNumber: number; stopIndex: number }
  | { kind: "passage"; key: string; dayKey: string; dayNumber: number; legIndex: number };

/**
 * One row per day. A stop's lay days are rows of their own; the day the boat
 * sails from a stop is the passage row for that leg, and it is also the day it
 * arrives at the next, so the arrival day is not a second row. A stop with no
 * nights between two legs adds no stop row. The last stop's rows run through
 * the day the boat leaves it, so the rows span the plan's first to last date;
 * a plan of one stop shows at least its arrival day.
 */
export function buildPlanRows(plan: Pick<Plan, "stops">): PlanRow[] {
  const { stops } = plan;
  if (stops.length === 0) return [];
  const first = stops[0].arrive;
  const day = (dayKey: string) => daysBetweenISO(first, dayKey) + 1;
  const rows: PlanRow[] = [];
  const stopRows = (stopIndex: number, from: number, count: number) => {
    for (let n = 0; n < count; n += 1) {
      const dayKey = addDaysISO(stops[stopIndex].arrive, from + n);
      rows.push({ kind: "stop", key: `stop-${stopIndex}-${n}`, dayKey, dayNumber: day(dayKey), stopIndex });
    }
  };
  const last = stops.length - 1;
  stopRows(0, 0, last === 0 ? stops[0].nights + 1 : stops[0].nights);
  for (let i = 0; i < last; i += 1) {
    const dayKey = stops[i].depart;
    rows.push({ kind: "passage", key: `leg-${i}`, dayKey, dayNumber: day(dayKey), legIndex: i });
    // The arrival day at the next stop is the passage row above.
    // The last stop also has its departure day, the plan's last date.
    stopRows(i + 1, 1, i + 1 === last ? stops[i + 1].nights : Math.max(stops[i + 1].nights - 1, 0));
  }
  return rows;
}

/**
 * The row a key points at in the plan as it is now, with its current date;
 * null when the plan no longer has that row.
 */
export function findPlanRow(plan: Pick<Plan, "stops">, key: string): PlanRow | null {
  return buildPlanRows(plan).find((r) => r.key === key) ?? null;
}

export type WindRelation = "ahead" | "abeam" | "astern";

/**
 * Where the wind is against the leg's course. `windFromDeg` is the direction
 * the wind blows from, so a wind from the course direction is dead ahead.
 * Within 60 degrees of the bow is ahead, within 60 of the stern is astern,
 * the rest abeam.
 */
export function windRelation(courseDeg: number, windFromDeg: number): WindRelation {
  const off = Math.abs(shortTurn(courseDeg, windFromDeg));
  if (off < 60) return "ahead";
  if (off > 120) return "astern";
  return "abeam";
}

/** Mean of compass bearings, so 350 and 10 average to 0 and not 180. Null for none. */
export function meanForecastBearing(degrees: number[]): number | null {
  // The forecast sends a negative bearing for a reading it does not have.
  const mean = circularMeanDegOrNull(degrees.filter((d) => Number.isFinite(d) && d >= 0));
  return mean === null ? null : Math.round(mean) % 360;
}

export interface DayWind {
  minKts: number;
  maxKts: number;
  /** Null when the forecast had no gust reading. */
  gustKts: number | null;
  fromDeg: number | null;
}

// The forecast sends a negative gust for a reading it does not have.
const gustOrNull = (g: number) => (Number.isFinite(g) && g >= 0 ? g : null);

/**
 * A day's wind from its hourly points; the day's own figures when the
 * provider sent no hours. Gust is the day's highest.
 */
export function summariseDayWind(
  hourly: Array<{ windSpeed: number; windGust: number; windDirectionDeg: number }>,
  day: { windSpeed: number; windGust: number },
): DayWind | null {
  // The forecast sends -1 for a reading it does not have.
  hourly = hourly.filter((h) => h.windSpeed >= 0);
  if (hourly.length === 0) {
    if (day.windSpeed < 0) return null;
    return { minKts: day.windSpeed, maxKts: day.windSpeed, gustKts: gustOrNull(day.windGust), fromDeg: null };
  }
  const speeds = hourly.map((h) => h.windSpeed);
  return {
    minKts: Math.min(...speeds),
    maxKts: Math.max(...speeds),
    gustKts: gustOrNull(Math.max(day.windGust, ...hourly.map((h) => h.windGust))),
    fromDeg: meanForecastBearing(hourly.map((h) => h.windDirectionDeg)),
  };
}

export interface HourlyWindPoint {
  hourOfDay: number;
  windSpeed: number;
  windGust: number;
  windDirectionDeg: number;
}

/**
 * The night's wind for a stop row on day D: the hours from 18:00 on D to
 * 06:00 on D+1, as the forecast gives them at the place. When D+1 is past the
 * forecast only the evening is used. With no hour in the window there is no
 * wind: the day's own figures are daytime ones and would be wrong here.
 */
export function summariseOvernightWind(
  evening: HourlyWindPoint[],
  morning: HourlyWindPoint[] | undefined,
): DayWind | null {
  const night = [
    ...evening.filter((h) => h.hourOfDay >= 18 && h.hourOfDay <= 23),
    ...(morning ?? []).filter((h) => h.hourOfDay >= 0 && h.hourOfDay < 6),
  ].filter((h) => h.windSpeed >= 0);
  if (night.length === 0) return null;
  const speeds = night.map((h) => h.windSpeed);
  return {
    minKts: Math.min(...speeds),
    maxKts: Math.max(...speeds),
    gustKts: gustOrNull(Math.max(...night.map((h) => h.windGust))),
    fromDeg: meanForecastBearing(night.map((h) => h.windDirectionDeg)),
  };
}

/** How far ahead the forecast reaches: about ten days. */
export const FORECAST_HORIZON_DAYS = 10;

/** Whether a plan day is inside the forecast's reach, counting from today (YYYY-MM-DD). */
export function withinForecastHorizon(dayKey: string, todayKey: string): boolean {
  return dayKey <= addDaysISO(todayKey, FORECAST_HORIZON_DAYS);
}

/** "12–18 kn", or "12 kn" when the range is one number. */
export function formatWindRange(minKts: number, maxKts: number): string {
  const lo = Math.round(minKts);
  const hi = Math.round(maxKts);
  return lo === hi ? `${lo} kn` : `${lo}–${hi} kn`;
}

/** "1.2–1.8 m", or "1.2 m". */
export function formatWaveRange(minM: number, maxM: number): string {
  const lo = minM.toFixed(1);
  const hi = maxM.toFixed(1);
  return lo === hi ? `${lo} m` : `${lo}–${hi} m`;
}

/**
 * Where a leg's forecast is read: the middle of its route, rounded to two
 * decimals (about a kilometre) so legs and stops near each other share a
 * cached forecast. A leg that did not route has no place: the straight line
 * between its ends is not the water the boat crosses.
 */
export function legForecastPlace(
  leg: PlanLeg | undefined,
): { lat: number; lon: number } | null {
  if (!leg || leg.route.length === 0) return null;
  const round = (v: number) => Math.round(v * 100) / 100;
  const [lat, lon] = leg.route[Math.floor(leg.route.length / 2)];
  return { lat: round(lat), lon: round(lon) };
}

export type DayCoverage = "inside" | "before" | "beyond" | "unknown";

/**
 * Whether a plan day falls inside the days the forecast covers. A day past the
 * last forecast day is "beyond": the table says "No forecast yet" and shows
 * nothing in its place. With no forecast days at all nothing is known.
 */
export function dayCoverage(forecastDayKeys: string[], dayKey: string): DayCoverage {
  if (forecastDayKeys.length === 0) return "unknown";
  if (forecastDayKeys.includes(dayKey)) return "inside";
  if (dayKey > forecastDayKeys[forecastDayKeys.length - 1]) return "beyond";
  if (dayKey < forecastDayKeys[0]) return "before";
  return "unknown";
}

/**
 * Arrival dates for a draft that is still being edited: the first arrival, then
 * each stop's arrival is the one before plus its nights. This only previews the
 * server's rule; the saved dates are the server's.
 */
export function previewArrivals(firstArrive: string, nights: number[]): string[] {
  const out: string[] = [];
  let day = firstArrive;
  for (const n of nights) {
    out.push(day);
    day = addDaysISO(day, n);
  }
  return out;
}

/** A stop as the editor holds it, before it is saved. */
export type DraftStop = Pick<PlanStop, "name" | "lat" | "lon" | "nights" | "vias" | "departTime">;

/**
 * The draft as the server takes it. No leg leaves the last stop, so it carries
 * no via places and no departure time whatever it held before it was last.
 */
export function draftToInput(name: string, firstArrive: string, stops: DraftStop[]): PlanInput {
  return {
    name: name.trim(),
    stops: stops.map((s, i) => {
      const last = i === stops.length - 1;
      return {
        name: s.name.trim(),
        lat: s.lat,
        lon: s.lon,
        // Only the first arrival is the operator's; the server dates the rest.
        arrive: i === 0 ? firstArrive : undefined,
        nights: s.nights,
        vias: last ? [] : s.vias,
        departTime: last ? "" : s.departTime,
      };
    }),
  };
}

/**
 * Vias and the departure time belong to the leg leaving a stop. After stops
 * are reordered or removed, a stop whose next stop is not the one it had keeps
 * neither: they would steer a different leg. Stops are matched by `id`.
 */
export function clearMovedLegs<T extends { id: string; vias: PlanVia[]; departTime: string }>(
  before: T[],
  after: T[],
): T[] {
  const nextBefore = new Map(before.map((s, i) => [s.id, before[i + 1]?.id ?? null]));
  return after.map((s, i) => {
    const next = after[i + 1]?.id ?? null;
    if (nextBefore.get(s.id) === next) return s;
    if (s.vias.length === 0 && s.departTime === "") return s;
    return { ...s, vias: [], departTime: "" };
  });
}
