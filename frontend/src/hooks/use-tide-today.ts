import { useEffect } from 'react';
import { useApiQuery } from '@/lib/api-query';

export interface TideToday {
  datetime: string;
  current_tide_height_ft: number;
  tide_direction: string;
  high_tide_time: string;
  high_tide_height_ft: number;
  low_tide_time: string;
  low_tide_height_ft: number;
  station_name: string;
  provider: string;
}

// Before the first fetch resolves, high/low are simply unknown - '' matches
// what a genuinely missing extreme reads as once fetched (see the fetch
// handler below), rather than a fabricated "now" a renderer could mistake
// for a real time.
const defaultTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: -1,
  tide_direction: '—',
  high_tide_time: '',
  high_tide_height_ft: -1,
  low_tide_time: '',
  low_tide_height_ft: -1,
  station_name: '',
  provider: '',
};

function normalise(data: Partial<TideToday>): TideToday {
  // Code-review finding: the sentinel for "not published" is exactly
  // -1, not "anything below -1" - a real height only ever becomes -1
  // here when the backend didn't send a number at all (Number.isFinite
  // guards against null/undefined/NaN), never because it happened to
  // be very negative. A real spring low can read below -1 ft on some
  // datums (low-water-clearance.ts's isUsableTideHeightFt makes the
  // same distinction on the read side).
  return {
    datetime: data.datetime || new Date().toISOString(),
    current_tide_height_ft:
      typeof data.current_tide_height_ft === 'number' && Number.isFinite(data.current_tide_height_ft)
        ? data.current_tide_height_ft
        : -1,
    tide_direction: data.tide_direction || '—',
    // A missing/unparseable time stays '' rather than becoming "now" -
    // substituting the current instant reads to the operator as a real
    // extreme (a fake "High · <now>"), which tideExtremesByTime/
    // nextTideExtreme (lib/tide-estimate.ts) and every renderer of this
    // field now treat as absent, not a real time to display.
    high_tide_time: typeof data.high_tide_time === 'string' ? data.high_tide_time : '',
    high_tide_height_ft:
      typeof data.high_tide_height_ft === 'number' && Number.isFinite(data.high_tide_height_ft)
        ? data.high_tide_height_ft
        : -1,
    low_tide_time: typeof data.low_tide_time === 'string' ? data.low_tide_time : '',
    low_tide_height_ft:
      typeof data.low_tide_height_ft === 'number' && Number.isFinite(data.low_tide_height_ft)
        ? data.low_tide_height_ft
        : -1,
    station_name: typeof data.station_name === 'string' ? data.station_name : '',
    provider: typeof data.provider === 'string' ? data.provider : '',
  };
}

export function useTideToday(refreshIntervalSeconds = 600) {
  const { data, error, isLoading } = useApiQuery<TideToday>({
    queryKey: ['/api/tide-today'],
    path: '/api/tide-today',
    refetchInterval: refreshIntervalSeconds * 1000,
    select: normalise,
  });

  // The tile keeps showing the last reading, so a failure is only logged.
  useEffect(() => {
    if (error) console.error('Error fetching tide:', error);
  }, [error]);

  return { tide: data ?? defaultTide, loading: isLoading };
}
