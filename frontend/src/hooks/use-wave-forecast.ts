import { useCallback, useEffect, useRef, useState } from 'react';

export interface WaveHourlyPoint {
  label: string;
  hourOfDay: number;
  waveHeightM: number;
  wavePeriodS: number;
  waveDirectionDeg: number;
  windWaveHeightM: number;
  swellWaveHeightM: number;
}

export interface WaveForecastDay {
  dayKey: string;
  date: string;
  dayName: string;
  waveSummary: string | null;
  hourlyWave: WaveHourlyPoint[];
}

interface WaveHourlyApi {
  label?: string;
  hour_of_day?: number;
  wave_height_m?: number;
  wave_period_s?: number;
  wave_direction_deg?: number;
  wind_wave_height_m?: number;
  swell_wave_height_m?: number;
}

interface WaveForecastDayApi {
  day_key?: string;
  date?: string;
  day_name?: string;
  wave_summary?: string;
  hourly_wave?: WaveHourlyApi[];
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
              }))
            : [],
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
