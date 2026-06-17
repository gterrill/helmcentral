import { useCallback, useEffect, useRef, useState } from 'react';

export interface WeatherHourlyWindPoint {
  label: string;
  windSpeed: number;
  windGust: number;
  windDirection: string;
  windDirectionDeg: number;
}

export interface WeatherHourlyWavePoint {
  label: string;
  waveHeightM: number;
  wavePeriodS: number;
  waveDirectionDeg: number;
  windWaveHeightM: number;
  swellWaveHeightM: number;
}

export interface WeatherHourlyPrecipPoint {
  label: string;
  precipChancePct: number;
  precipIntensityMm: number;
}

export interface WeatherHourlyUVPoint {
  label: string;
  uvIndex: number;
}

export interface WeatherForecastDay {
  date: string;
  dayName: string;
  condition: string;
  high: number;
  low: number;
  windSpeed: number;
  windGust: number;
  windDirection: string;
  windSummary: string | null;
  waveSummary: string | null;
  precipitationSummary: string | null;
  precipitation: number;
  sunriseTime: string | null;
  sunsetTime: string | null;
  moonPhase: string | null;
  hourlyWind: WeatherHourlyWindPoint[];
  hourlyWave: WeatherHourlyWavePoint[];
  hourlyPrecip: WeatherHourlyPrecipPoint[];
  hourlyUV: WeatherHourlyUVPoint[];
}

export interface WeatherHourlyEntry {
  label: string;
  condition: string;
  temperatureF: number;
  kind: 'forecast' | 'sunset' | string;
}

interface WeatherHourlyWindApi {
  label?: string;
  wind_speed_kts?: number;
  wind_gust_kts?: number;
  wind_direction?: string;
  wind_direction_deg?: number;
}

interface WeatherHourlyWaveApi {
  label?: string;
  wave_height_m?: number;
  wave_period_s?: number;
  wave_direction_deg?: number;
  wind_wave_height_m?: number;
  swell_wave_height_m?: number;
}

interface WeatherHourlyPrecipApi {
  label?: string;
  precipitation_chance_pct?: number;
  precipitation_intensity_mm?: number;
}

interface WeatherHourlyUVApi {
  label?: string;
  uv_index?: number;
}

interface WeatherForecastDayApi {
  date?: string;
  day_name?: string;
  condition?: string;
  high_temp_f?: number;
  low_temp_f?: number;
  wind_speed_kts?: number;
  wind_gust_kts?: number;
  wind_direction?: string;
  wind_summary?: string;
  wave_summary?: string;
  precipitation_summary?: string;
  precipitation_pct?: number;
  sunrise_time?: string;
  sunset_time?: string;
  moon_phase?: string;
  hourly_wind?: WeatherHourlyWindApi[];
  hourly_wave?: WeatherHourlyWaveApi[];
  hourly_precip?: WeatherHourlyPrecipApi[];
  hourly_uv?: WeatherHourlyUVApi[];
}

interface WeatherForecastEnvelopeApi {
  days?: WeatherForecastDayApi[];
  hourly_today?: Array<{
    label?: string;
    condition?: string;
    temperature_f?: number;
    kind?: string;
  }>;
  summary?: string;
  cached?: boolean;
  updated_at?: string;
  ttl_seconds?: number;
}

export function useWeatherForecast(refreshIntervalSeconds = 3600) {
  const [forecast, setForecast] = useState<WeatherForecastDay[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [hourlyToday, setHourlyToday] = useState<WeatherHourlyEntry[]>([]);
  const [summary, setSummary] = useState<string | null>(null);
  const [isCached, setIsCached] = useState(false);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [ttlSeconds, setTtlSeconds] = useState<number | null>(null);
  const hasLoadedDataRef = useRef(false);

  const fetchForecast = useCallback(async () => {
      try {
        if (!hasLoadedDataRef.current) {
          setLoading(true);
        }

        const response = await fetch('/api/weather-forecast');
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }

        const payload = (await response.json()) as WeatherForecastDayApi[] | WeatherForecastEnvelopeApi;
        const rawDays = Array.isArray(payload) ? payload : payload.days;
        if (!Array.isArray(rawDays)) {
          throw new Error('Unexpected weather forecast payload format');
        }

        const mappedForecast = rawDays
          .slice(0, 10)
          .map((day, idx): WeatherForecastDay => {
            const fallbackDate = new Date();
            fallbackDate.setDate(fallbackDate.getDate() + idx);

            const windSpeed = typeof day.wind_speed_kts === 'number' ? day.wind_speed_kts : -1;
            const windGust = typeof day.wind_gust_kts === 'number' ? day.wind_gust_kts : windSpeed;

            return {
              date: day.date || fallbackDate.toLocaleDateString('en-US', { month: 'short', day: 'numeric' }),
              dayName: day.day_name || fallbackDate.toLocaleDateString('en-US', { weekday: 'long' }),
              condition: day.condition || 'Unknown',
              high: typeof day.high_temp_f === 'number' ? day.high_temp_f : -1,
              low: typeof day.low_temp_f === 'number' ? day.low_temp_f : -1,
              windSpeed,
              windGust,
              windDirection: day.wind_direction || '—',
              windSummary: typeof day.wind_summary === 'string' && day.wind_summary !== '' ? day.wind_summary : null,
              waveSummary: typeof day.wave_summary === 'string' && day.wave_summary !== '' ? day.wave_summary : null,
              precipitationSummary: typeof day.precipitation_summary === 'string' && day.precipitation_summary !== '' ? day.precipitation_summary : null,
              precipitation: typeof day.precipitation_pct === 'number' ? day.precipitation_pct : 0,
              sunriseTime: typeof day.sunrise_time === 'string' && day.sunrise_time !== '' ? day.sunrise_time : null,
              sunsetTime: typeof day.sunset_time === 'string' && day.sunset_time !== '' ? day.sunset_time : null,
              moonPhase: typeof day.moon_phase === 'string' && day.moon_phase !== '' ? day.moon_phase : null,
              hourlyWind: Array.isArray(day.hourly_wind)
                ? day.hourly_wind.map((entry) => ({
                    label: entry.label || '—',
                    windSpeed: typeof entry.wind_speed_kts === 'number' ? entry.wind_speed_kts : -1,
                    windGust: typeof entry.wind_gust_kts === 'number' ? entry.wind_gust_kts : -1,
                    windDirection: entry.wind_direction || '—',
                    windDirectionDeg: typeof entry.wind_direction_deg === 'number' ? entry.wind_direction_deg : -1,
                  }))
                : [],
              hourlyWave: Array.isArray(day.hourly_wave)
                ? day.hourly_wave.map((entry) => ({
                    label: entry.label || '—',
                    waveHeightM: typeof entry.wave_height_m === 'number' ? entry.wave_height_m : -1,
                    wavePeriodS: typeof entry.wave_period_s === 'number' ? entry.wave_period_s : -1,
                    waveDirectionDeg: typeof entry.wave_direction_deg === 'number' ? entry.wave_direction_deg : -1,
                    windWaveHeightM: typeof entry.wind_wave_height_m === 'number' ? entry.wind_wave_height_m : -1,
                    swellWaveHeightM: typeof entry.swell_wave_height_m === 'number' ? entry.swell_wave_height_m : -1,
                  }))
                : [],
              hourlyPrecip: Array.isArray(day.hourly_precip)
                ? day.hourly_precip.map((entry) => ({
                    label: entry.label || '—',
                    precipChancePct: typeof entry.precipitation_chance_pct === 'number' ? entry.precipitation_chance_pct : 0,
                    precipIntensityMm: typeof entry.precipitation_intensity_mm === 'number' ? entry.precipitation_intensity_mm : 0,
                  }))
                : [],
              hourlyUV: Array.isArray(day.hourly_uv)
                ? day.hourly_uv.map((entry) => ({
                    label: entry.label || '—',
                    uvIndex: typeof entry.uv_index === 'number' ? entry.uv_index : 0,
                  }))
                : [],
            };
          });

        if (mappedForecast.length > 0) {
          hasLoadedDataRef.current = true;
        }

        setForecast(mappedForecast);
        if (!Array.isArray(payload)) {
          setHourlyToday(Array.isArray(payload.hourly_today)
            ? payload.hourly_today.map((entry) => ({
                label: entry.label || '—',
                condition: entry.condition || 'Unknown',
                temperatureF: typeof entry.temperature_f === 'number' ? entry.temperature_f : -1,
                kind: entry.kind || 'forecast',
              }))
            : []);
          setSummary(typeof payload.summary === 'string' ? payload.summary : null);
          setIsCached(Boolean(payload.cached));
          setUpdatedAt(typeof payload.updated_at === 'string' ? payload.updated_at : null);
          setTtlSeconds(typeof payload.ttl_seconds === 'number' ? payload.ttl_seconds : null);
        } else {
          setHourlyToday([]);
          setSummary(null);
          setIsCached(false);
          setUpdatedAt(null);
          setTtlSeconds(null);
        }
        setError(null);
      } catch (error) {
        console.error('Error fetching weather forecast:', error);
        setError(error instanceof Error ? error.message : 'Failed to load forecast data');
      } finally {
        setLoading(false);
      }
    }, []);

  useEffect(() => {
    fetchForecast();
    const interval = setInterval(fetchForecast, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [fetchForecast, refreshIntervalSeconds]);

  return { forecast, hourlyToday, summary, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchForecast };
}
