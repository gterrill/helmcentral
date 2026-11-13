import { useCallback, useContext, useMemo, useSyncExternalStore } from "react";

import type { ForecastPlace } from "@/lib/forecast-place";
import { compassPointFor } from "@/lib/format";
import { addDaysISO } from "@/lib/stay";
import {
  dayCoverage,
  summariseDayWind,
  summariseOvernightWind,
  type DayCoverage,
  type DayWind,
} from "@/lib/plans";
import { PlanForecastContext, placeKey, type PlaceFeed } from "@/lib/plan-forecast-store";

export interface PlanDayForecast {
  loading: boolean;
  error: string | null;
  coverage: DayCoverage;
  wind: DayWind | null;
  /** Compass name of the day's wind, as the forecast names it. */
  windDirection: string | null;
  waveMinM: number | null;
  waveMaxM: number | null;
  /** The wave feed is separate from the weather: it loads and fails on its own. */
  wavesLoading: boolean;
  wavesError: string | null;
}

const WAITING: PlanDayForecast = {
  loading: true,
  error: null,
  coverage: "unknown",
  wind: null,
  windDirection: null,
  waveMinM: null,
  waveMaxM: null,
  wavesLoading: true,
  wavesError: null,
};

const NO_PLACE: PlanDayForecast = { ...WAITING, loading: false, wavesLoading: false };

/** A passage row reads the whole day; a stop row reads the night that follows it. */
export type DayWindow = "day" | "overnight";

export function dayForecast(feed: PlaceFeed, dayKey: string, span: DayWindow = "day"): PlanDayForecast {
  const { weather, waves } = feed;
  const coverage = dayCoverage(
    weather.forecast.map((d) => d.dayKey),
    dayKey,
  );
  const day = coverage === "inside" ? weather.forecast.find((d) => d.dayKey === dayKey) : undefined;
  const nextDay = span === "overnight" ? weather.forecast.find((d) => d.dayKey === addDaysISO(dayKey, 1)) : undefined;
  const wind = !day
    ? null
    : span === "overnight"
      ? summariseOvernightWind(day.hourlyWind, nextDay?.hourlyWind)
      : summariseDayWind(day.hourlyWind, day);
  const waveDay = waves.days.find((d) => d.dayKey === dayKey);
  const waveHeights = (waveDay?.hourlyWave ?? [])
    .map((h) => h.waveHeightM)
    .filter((m) => Number.isFinite(m) && m >= 0);
  return {
    loading: weather.loading,
    error: weather.error,
    coverage,
    wind,
    // The night's direction is the night's own mean, named as the day's is.
    windDirection:
      span === "overnight"
        ? wind?.fromDeg != null
          ? compassPointFor(wind.fromDeg)
          : null
        : day
          ? day.windDirection
          : null,
    waveMinM: waveHeights.length > 0 ? Math.min(...waveHeights) : null,
    waveMaxM: waveHeights.length > 0 ? Math.max(...waveHeights) : null,
    wavesLoading: waves.loading,
    wavesError: waves.error,
  };
}

/**
 * One plan day's forecast at one place, read from the plan table's feed for
 * that place (the table reads each distinct place once). A day the forecast
 * does not reach has no figures: the row says "No forecast yet". With no place
 * there is nothing to read.
 */
export function usePlanDayForecast(
  place: ForecastPlace | null,
  dayKey: string,
  span: DayWindow = "day",
): PlanDayForecast {
  const store = useContext(PlanForecastContext);
  if (store === null) throw new Error("usePlanDayForecast needs a plan table above it");
  const key = place === null ? null : placeKey(place);
  const subscribe = useCallback(
    (notify: () => void) => (key === null ? () => {} : store.subscribe(key, notify)),
    [store, key],
  );
  const feed = useSyncExternalStore(subscribe, () => (key === null ? undefined : store.get(key)));
  return useMemo(() => {
    if (key === null) return NO_PLACE;
    // The feed is written just after the table first renders.
    return feed ? dayForecast(feed, dayKey, span) : WAITING;
  }, [key, feed, dayKey, span]);
}
