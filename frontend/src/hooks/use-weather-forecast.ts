import { useCallback, useEffect, useRef, useState } from 'react';

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

interface WeatherForecastEnvelopeApi {
  days?: WeatherForecastDayApi[];
  cached?: boolean;
  updated_at?: string;
  ttl_seconds?: number;
}

export function useWeatherForecast(refreshIntervalSeconds = 3600) {
  const [forecast, setForecast] = useState<WeatherForecastDay[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
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

        if (mappedForecast.length > 0) {
          hasLoadedDataRef.current = true;
        }

        setForecast(mappedForecast);
        if (!Array.isArray(payload)) {
          setIsCached(Boolean(payload.cached));
          setUpdatedAt(typeof payload.updated_at === 'string' ? payload.updated_at : null);
          setTtlSeconds(typeof payload.ttl_seconds === 'number' ? payload.ttl_seconds : null);
        } else {
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

  return { forecast, loading, error, isCached, updatedAt, ttlSeconds, refetch: fetchForecast };
}
