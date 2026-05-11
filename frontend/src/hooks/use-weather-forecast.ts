import { useCallback, useEffect, useState } from 'react';

export interface WeatherForecastDay {
  date: string;
  dayName: string;
  condition: string;
  high: number;
  low: number;
  windSpeed: number;
  windGust: number;
  windDirection: string;
  precipitation: number;
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
  precipitation_pct?: number;
}

export function useWeatherForecast(refreshIntervalSeconds = 600) {
  const [forecast, setForecast] = useState<WeatherForecastDay[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchForecast = useCallback(async () => {
      try {
        setLoading(true);
        setError(null);
        const response = await fetch('/api/weather-forecast');
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }

        const data = (await response.json()) as WeatherForecastDayApi[];
        if (!Array.isArray(data)) {
          throw new Error('Unexpected weather forecast payload format');
        }

        const mappedForecast = data
          .slice(0, 6)
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
              precipitation: typeof day.precipitation_pct === 'number' ? day.precipitation_pct : 0,
            };
          });

        setForecast(mappedForecast);
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

  return { forecast, loading, error, refetch: fetchForecast };
}
