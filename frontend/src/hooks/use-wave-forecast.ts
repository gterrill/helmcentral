import { useCallback, useEffect, useRef, useState } from 'react';

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

export function useWaveForecast(refreshIntervalSeconds = 3600) {
  const [days, setDays] = useState<WaveForecastDay[]>([]);
  const [seaTemperatureF, setSeaTemperatureF] = useState<number | null>(null);
  const [provider, setProvider] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isCached, setIsCached] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [ttlSeconds, setTtlSeconds] = useState<number | null>(null);
  const hasLoadedDataRef = useRef(false);

  const fetchWaveForecast = useCallback(async () => {
    try {
      if (!hasLoadedDataRef.current) {
        setLoading(true);
      }

      const response = await fetch('/api/wave-forecast');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const payload = (await response.json()) as WaveForecastEnvelopeApi;
      const rawDays = Array.isArray(payload.days) ? payload.days : [];

      const mappedDays = rawDays.slice(0, 10).map((day, idx): WaveForecastDay => {
        const fallbackDate = new Date();
        fallbackDate.setDate(fallbackDate.getDate() + idx);

        return {
          dayKey: typeof day.day_key === 'string' && day.day_key !== '' ? day.day_key : fallbackDate.toISOString().slice(0, 10),
          date: day.date || fallbackDate.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
          dayName: day.day_name || fallbackDate.toLocaleDateString('en-US', { weekday: 'long' }),
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

      if (mappedDays.length > 0) {
        hasLoadedDataRef.current = true;
      }

      setDays(mappedDays);
      setSeaTemperatureF(typeof payload.sea_temperature_f === 'number' ? payload.sea_temperature_f : null);
      setProvider(typeof payload.provider === 'string' && payload.provider !== '' ? payload.provider : null);
      setIsCached(Boolean(payload.cached));
      setUpdatedAt(typeof payload.updated_at === 'string' ? payload.updated_at : null);
      setTtlSeconds(typeof payload.ttl_seconds === 'number' ? payload.ttl_seconds : null);
      setError(null);
    } catch (error) {
      console.error('Error fetching wave forecast:', error);
      setError(error instanceof Error ? error.message : 'Failed to load wave forecast data');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchWaveForecast();
    const interval = setInterval(fetchWaveForecast, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [fetchWaveForecast, refreshIntervalSeconds]);

  return { days, seaTemperatureF, provider, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchWaveForecast };
}
