import { useState, useEffect } from 'react';

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

export function useWeatherToday(refreshIntervalSeconds = 600) {
  const [weather, setWeather] = useState<WeatherToday>(defaultWeather);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchWeather = async () => {
      try {
        setLoading(true);
        const response = await fetch('/api/weather-today');
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();

        // Validate required numeric fields
        const validWeather: WeatherToday = {
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

        setWeather(validWeather);
      } catch (error) {
        console.error('Error fetching weather:', error);
      } finally {
        setLoading(false);
      }
    };

    fetchWeather();
    const interval = setInterval(fetchWeather, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [refreshIntervalSeconds]);

  return { weather, loading };
}
