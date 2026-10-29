import { useEffect, useMemo } from 'react';
import { declareQuery, useApiQuery } from '@/lib/api-query';
import { calendarDayLabel, requireDayKey } from '@/lib/vessel-time';
import { forecastQuery, type ForecastQueryOptions } from '@/lib/forecast-place';

export type WaveSteepnessBand = 'rolling' | 'building' | 'steep' | 'breaking';

export interface WaveHourlyPoint {
  label: string;
  hourOfDay: number;
  waveHeightM: number;
  wavePeriodS: number;
  waveDirectionDeg: number;
  windWaveHeightM: number;
  swellWaveHeightM: number;
  /**
   * Per-component direction and period. A component with a zero period is
   * absent, not a wave train heading due north - that is how the provider
   * itself encodes a flat component.
   */
  windWaveDirectionDeg: number;
  windWavePeriodS: number;
  swellWaveDirectionDeg: number;
  swellWavePeriodS: number;
  /**
   * Wave height over deep-water wavelength - the number that says whether
   * seas break, which height alone does not. null when the hour carried no
   * period for the backend to derive it from; a 0 here would read as a
   * glassy sea rather than as a missing reading.
   */
  steepnessRatio: number | null;
  steepnessBand: WaveSteepnessBand | null;
}

/**
 * Leading indicators the backend derives from the hourly series, each a
 * documented threshold from Surviving the Storm rather than a house rule.
 */
export interface WaveDayIndicators {
  /** Seas rose 3m inside three hours - the criterion NOAA's Scott Prosise used to find dynamic-fetch events. */
  waveFront: boolean;
  /** Height and period both up 50% in an hour, which Lee Chesneau calls a certain danger signal. */
  rapidBuild: boolean;
  /** Period lengthened 3s or more in an hour, a leading sign of an arriving wave front. */
  periodStep: boolean;
  /** Wind wave and swell running more than 60 degrees apart. */
  crossSea: boolean;
}

export interface WaveForecastDay {
  dayKey: string;
  date: string;
  dayName: string;
  waveSummary: string | null;
  hourlyWave: WaveHourlyPoint[];
  indicators: WaveDayIndicators;
}

interface WaveHourlyApi {
  label?: string;
  hour_of_day?: number;
  wave_height_m?: number;
  wave_period_s?: number;
  wave_direction_deg?: number;
  wind_wave_height_m?: number;
  swell_wave_height_m?: number;
  wind_wave_direction_deg?: number;
  wind_wave_period_s?: number;
  swell_wave_direction_deg?: number;
  swell_wave_period_s?: number;
  steepness_ratio?: number | null;
  steepness_band?: string | null;
}

const WAVE_STEEPNESS_BANDS: readonly WaveSteepnessBand[] = ['rolling', 'building', 'steep', 'breaking'];

function steepnessBandOrNull(value: string | null | undefined): WaveSteepnessBand | null {
  return WAVE_STEEPNESS_BANDS.includes(value as WaveSteepnessBand) ? (value as WaveSteepnessBand) : null;
}

interface WaveForecastDayApi {
  day_key?: string;
  date?: string;
  day_name?: string;
  wave_summary?: string;
  hourly_wave?: WaveHourlyApi[];
  indicators?: { wave_front?: boolean; rapid_build?: boolean; period_step?: boolean; cross_sea?: boolean };
}

interface WaveForecastEnvelopeApi {
  provider?: string;
  days?: WaveForecastDayApi[];
  sea_temperature_f?: number | null;
  cached?: boolean;
  updated_at?: string;
  ttl_seconds?: number;
}

interface MappedWaveForecast {
  days: WaveForecastDay[];
  seaTemperatureF: number | null;
  provider: string | null;
  isCached: boolean;
  updatedAt: string | null;
  ttlSeconds: number | null;
}

function mapWavePayload(payload: WaveForecastEnvelopeApi): MappedWaveForecast {
  const rawDays = Array.isArray(payload.days) ? payload.days : [];

  const mappedDays = rawDays.slice(0, 10).map((day): WaveForecastDay => {
    const dayKey = requireDayKey(day.day_key);
    return {
      dayKey,
      date: day.date || calendarDayLabel(dayKey, { month: 'short', day: 'numeric' }),
      dayName: day.day_name || calendarDayLabel(dayKey, { weekday: 'long' }),
      waveSummary: typeof day.wave_summary === 'string' && day.wave_summary !== '' ? day.wave_summary : null,
      hourlyWave: Array.isArray(day.hourly_wave)
        ? day.hourly_wave.map((entry) => ({
            label: entry.label || '—',
            hourOfDay: typeof entry.hour_of_day === 'number' ? entry.hour_of_day : -1,
            waveHeightM: typeof entry.wave_height_m === 'number' ? entry.wave_height_m : -1,
            wavePeriodS: typeof entry.wave_period_s === 'number' ? entry.wave_period_s : -1,
            waveDirectionDeg: typeof entry.wave_direction_deg === 'number' ? entry.wave_direction_deg : -1,
            windWaveHeightM: typeof entry.wind_wave_height_m === 'number' ? entry.wind_wave_height_m : -1,
            swellWaveHeightM: typeof entry.swell_wave_height_m === 'number' ? entry.swell_wave_height_m : -1,
            windWaveDirectionDeg: typeof entry.wind_wave_direction_deg === 'number' ? entry.wind_wave_direction_deg : -1,
            windWavePeriodS: typeof entry.wind_wave_period_s === 'number' ? entry.wind_wave_period_s : -1,
            swellWaveDirectionDeg: typeof entry.swell_wave_direction_deg === 'number' ? entry.swell_wave_direction_deg : -1,
            swellWavePeriodS: typeof entry.swell_wave_period_s === 'number' ? entry.swell_wave_period_s : -1,
            steepnessRatio: typeof entry.steepness_ratio === 'number' ? entry.steepness_ratio : null,
            steepnessBand: steepnessBandOrNull(entry.steepness_band),
          }))
        : [],
      indicators: {
        waveFront: Boolean(day.indicators?.wave_front),
        rapidBuild: Boolean(day.indicators?.rapid_build),
        periodStep: Boolean(day.indicators?.period_step),
        crossSea: Boolean(day.indicators?.cross_sea),
      },
    };
  });

  return {
    days: mappedDays,
    seaTemperatureF: typeof payload.sea_temperature_f === 'number' ? payload.sea_temperature_f : null,
    provider: typeof payload.provider === 'string' && payload.provider !== '' ? payload.provider : null,
    isCached: Boolean(payload.cached),
    updatedAt: typeof payload.updated_at === 'string' ? payload.updated_at : null,
    ttlSeconds: typeof payload.ttl_seconds === 'number' ? payload.ttl_seconds : null,
  };
}

const EMPTY_WAVES: MappedWaveForecast = {
  days: [],
  seaTemperatureF: null,
  provider: null,
  isCached: false,
  updatedAt: null,
  ttlSeconds: null,
};

declareQuery(['/api/wave-forecast'], { structuralSharing: true });

export function useWaveForecast(refreshIntervalSeconds = 3600, options: ForecastQueryOptions = {}) {
  const { data, error, isLoading, refetch } = useApiQuery<MappedWaveForecast>({
    ...forecastQuery('/api/wave-forecast', options.place),
    refetchInterval: refreshIntervalSeconds * 1000,
    enabled: options.enabled,
    select: mapWavePayload,
  });

  useEffect(() => {
    if (error) console.error('Error fetching wave forecast:', error);
  }, [error]);

  // One object while the answer, the error and the loading state hold, so a
  // reader that memoizes on it (the Forecast page's props) holds too.
  return useMemo(() => {
    const { days, seaTemperatureF, provider, isCached, updatedAt, ttlSeconds } = data ?? EMPTY_WAVES;
    return {
      days,
      seaTemperatureF,
      provider,
      loading: isLoading,
      error: error ? error.message || 'Failed to load wave forecast data' : null,
      isCached,
      updatedAt,
      ttlSeconds,
      refetch,
    };
  }, [data, error, isLoading, refetch]);
}
