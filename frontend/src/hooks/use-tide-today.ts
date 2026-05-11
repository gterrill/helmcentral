import { useEffect, useState } from 'react';

export interface TideToday {
  datetime: string;
  current_tide_height_ft: number;
  tide_direction: string;
  high_tide_time: string;
  high_tide_height_ft: number;
  low_tide_time: string;
  low_tide_height_ft: number;
}

const defaultTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: -1,
  tide_direction: '—',
  high_tide_time: new Date().toISOString(),
  high_tide_height_ft: -1,
  low_tide_time: new Date().toISOString(),
  low_tide_height_ft: -1,
};

export function useTideToday(refreshIntervalSeconds = 600) {
  const [tide, setTide] = useState<TideToday>(defaultTide);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchTide = async () => {
      try {
        setLoading(true);
        const response = await fetch('/api/tide-today');
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`);
        }
        const data = await response.json();

        const validTide: TideToday = {
          datetime: data.datetime || new Date().toISOString(),
          current_tide_height_ft:
            typeof data.current_tide_height_ft === 'number' && data.current_tide_height_ft >= -1
              ? data.current_tide_height_ft
              : -1,
          tide_direction: data.tide_direction || '—',
          high_tide_time: data.high_tide_time || new Date().toISOString(),
          high_tide_height_ft:
            typeof data.high_tide_height_ft === 'number' && data.high_tide_height_ft >= -1
              ? data.high_tide_height_ft
              : -1,
          low_tide_time: data.low_tide_time || new Date().toISOString(),
          low_tide_height_ft:
            typeof data.low_tide_height_ft === 'number' && data.low_tide_height_ft >= -1
              ? data.low_tide_height_ft
              : -1,
        };

        setTide(validTide);
      } catch (error) {
        console.error('Error fetching tide:', error);
      } finally {
        setLoading(false);
      }
    };

    fetchTide();
    const interval = setInterval(fetchTide, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [refreshIntervalSeconds]);

  return { tide, loading };
}
