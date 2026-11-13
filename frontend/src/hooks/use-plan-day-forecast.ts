import { useCallback, useContext, useMemo, useSyncExternalStore } from "react";

import type { ForecastPlace } from "@/lib/forecast-place";
import {
  dayCoverage,
  summariseDayWind,
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

export function dayForecast(feed: PlaceFeed, dayKey: string): PlanDayForecast {
  const { weather, waves } = feed;
  const coverage = dayCoverage(
    weather.forecast.map((d) => d.dayKey),
    dayKey,
  );
  const day = coverage === "inside" ? weather.forecast.find((d) => d.dayKey === dayKey) : undefined;
  const waveDay = waves.days.find((d) => d.dayKey === dayKey);
  const waveHeights = (waveDay?.hourlyWave ?? [])
    .map((h) => h.waveHeightM)
    .filter((m) => Number.isFinite(m) && m >= 0);
  return {
    loading: weather.loading,
    error: weather.error,
    coverage,
    wind: day ? summariseDayWind(day.hourlyWind, day) : null,
    windDirection: day ? day.windDirection : null,
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
export function usePlanDayForecast(place: ForecastPlace | null, dayKey: string): PlanDayForecast {
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
    return feed ? dayForecast(feed, dayKey) : WAITING;
  }, [key, feed, dayKey]);
}
