import { useEffect } from 'react';
import { useApiQuery } from '@/lib/api-query';

export interface WeatherToday {
  datetime: string;
  temperature_f: number;
  condition: string;
  wind_speed_kts: number;
  wind_gust_kts: number;
  wind_direction: string;
  precipitation_pct: number;
  provider: string;
  cached: boolean;
  updated_at: string;
  ttl_seconds: number;
}

const defaultWeather: WeatherToday = {
  datetime: new Date().toISOString(),
  temperature_f: -1,
  condition: 'Loading...',
  wind_speed_kts: -1,
  wind_gust_kts: -1,
  wind_direction: '—',
  precipitation_pct: -1,
  provider: '',
  cached: false,
  updated_at: '',
  ttl_seconds: 0,
};

function normalise(data: Partial<WeatherToday>): WeatherToday {
  // Validate required numeric fields
  return {
    datetime: data.datetime || new Date().toISOString(),
    temperature_f:
      typeof data.temperature_f === 'number' && data.temperature_f >= -1
        ? data.temperature_f
        : -1,
    condition: data.condition || 'Unknown',
    wind_speed_kts:
      typeof data.wind_speed_kts === 'number' && data.wind_speed_kts >= -1
        ? data.wind_speed_kts
        : -1,
    wind_gust_kts:
      typeof data.wind_gust_kts === 'number' && data.wind_gust_kts >= -1
        ? data.wind_gust_kts
        : -1,
    wind_direction: data.wind_direction || '—',
    precipitation_pct:
      typeof data.precipitation_pct === 'number' && data.precipitation_pct >= -1
        ? data.precipitation_pct
        : -1,
    provider: typeof data.provider === 'string' ? data.provider : '',
    cached: Boolean(data.cached),
    updated_at: typeof data.updated_at === 'string' ? data.updated_at : '',
    ttl_seconds: typeof data.ttl_seconds === 'number' ? data.ttl_seconds : 0,
  };
}

export function useWeatherToday(refreshIntervalSeconds = 600) {
  const { data, error, isLoading } = useApiQuery<WeatherToday>({
    queryKey: ['/api/weather-today'],
    path: '/api/weather-today',
    refetchInterval: refreshIntervalSeconds * 1000,
    select: normalise,
    structuralSharing: true,
  });

  // The tile keeps showing the last reading, so a failure is only logged.
  useEffect(() => {
    if (error) console.error('Error fetching weather:', error);
  }, [error]);

  const weather = data ?? defaultWeather;
  return { weather, loading: isLoading };
}
