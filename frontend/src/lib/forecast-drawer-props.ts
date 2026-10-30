import type { ForecastDrawerProps } from "@/components/forecast-drawer";
import type { useForecastWarnings } from "@/hooks/use-forecast-warnings";
import type { useUpperAir } from "@/hooks/use-upper-air";
import type { useWaveForecast } from "@/hooks/use-wave-forecast";
import type { useWeatherForecast } from "@/hooks/use-weather-forecast";

/** What the forecast page shows for the boat: everything but the stay. */
export type ForecastBoatProps = Omit<ForecastDrawerProps, "stay">;

export interface ForecastFeeds {
  weather: ReturnType<typeof useWeatherForecast>;
  waves: ReturnType<typeof useWaveForecast>;
  upperAir: ReturnType<typeof useUpperAir>;
  warnings: ReturnType<typeof useForecastWarnings>;
  unit: "imperial" | "metric";
}

/**
 * Maps the four forecast feeds onto the drawer's props. The boat's feeds and
 * a stay's feeds are the same hooks pointed at different places, so both go
 * through this one mapping.
 */
export function buildForecastDrawerProps({
  weather,
  waves,
  upperAir,
  warnings,
  unit,
}: ForecastFeeds): ForecastBoatProps {
  return {
    forecast: weather.forecast,
    hourlyToday: weather.hourlyToday,
    summary: weather.summary,
    loading: weather.loading,
    error: weather.error,
    provider: weather.provider,
    isCached: weather.isCached,
    updatedAt: weather.updatedAt,
    ttlSeconds: weather.ttlSeconds,
    onRetry: weather.refetch,
    unit,
    activeForecastWarning: warnings.activeWarning,
    waveDays: waves.days,
    upperAirDays: upperAir.days,
    upperAirSeries: upperAir.series,
    upperAirWindow: upperAir.windowBand,
    upperAirUpdatedAt: upperAir.updatedAt,
    upperAirTtlSeconds: upperAir.ttlSeconds,
    waveSeaTemperatureF: waves.seaTemperatureF,
    waveLoading: waves.loading,
    waveError: waves.error,
    waveProvider: waves.provider,
    waveFallbackModel: waves.fallbackModel,
    waveNoData: waves.noData,
    waveIsCached: waves.isCached,
    waveUpdatedAt: waves.updatedAt,
    waveTtlSeconds: waves.ttlSeconds,
    onWaveRetry: waves.refetch,
  };
}
