import { FORECAST_REFRESH_SECONDS } from "@/config/app-config";
import { useWaveForecast } from "@/hooks/use-wave-forecast";
import { useWeatherForecast } from "@/hooks/use-weather-forecast";
import {
  dayCoverage,
  summariseDayWind,
  type DayCoverage,
  type DayWind,
} from "@/lib/plans";

export interface PlanDayForecast {
  loading: boolean;
  error: string | null;
  coverage: DayCoverage;
  wind: DayWind | null;
  /** Compass name of the day's wind, as the forecast names it. */
  windDirection: string | null;
  waveMinM: number | null;
  waveMaxM: number | null;
}

/**
 * One plan day's forecast at one place. The feeds are the Forecast page's own
 * place queries, so rows at the same rounded place share one request. A day
 * the forecast does not reach has no figures: the row says "No forecast yet".
 */
export function usePlanDayForecast(
  place: { lat: number; lon: number } | null,
  dayKey: string,
): PlanDayForecast {
  // No place, no request: a leg that did not route has nowhere to read.
  const enabled = place !== null;
  const weather = useWeatherForecast(FORECAST_REFRESH_SECONDS, { place: place ?? undefined, enabled });
  const waves = useWaveForecast(FORECAST_REFRESH_SECONDS, { place: place ?? undefined, enabled });

  const coverage = dayCoverage(
    weather.forecast.map((d) => d.dayKey),
    dayKey,
  );
  const day =
    coverage === "inside"
      ? weather.forecast.find((d) => d.dayKey === dayKey)
      : undefined;
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
  };
}
