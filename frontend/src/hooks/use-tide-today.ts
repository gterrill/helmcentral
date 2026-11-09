import { useEffect } from 'react';
import { declareQuery, useApiQuery } from '@/lib/api-query';
import { CHART_DATUM, normaliseTideDatum, type TideDatum } from '@/lib/tide-datum';

export interface TideToday {
  datetime: string;
  /** Feet, or null when there is none. Any number, negative included, is a reading. */
  current_tide_height_ft: number | null;
  tide_direction: string;
  high_tide_time: string;
  high_tide_height_ft: number | null;
  low_tide_time: string;
  low_tide_height_ft: number | null;
  station_name: string;
  provider: string;
  /** What every height above is measured from; an absent field is chart datum. */
  height_datum?: TideDatum;
}

// Before the first fetch resolves, high/low are simply unknown - '' matches
// what a genuinely missing extreme reads as once fetched (see the fetch
// handler below), rather than a fabricated "now" a renderer could mistake
// for a real time.
export const defaultTide: TideToday = {
  datetime: new Date().toISOString(),
  current_tide_height_ft: null,
  tide_direction: '—',
  high_tide_time: '',
  high_tide_height_ft: null,
  low_tide_time: '',
  low_tide_height_ft: null,
  station_name: '',
  provider: '',
  height_datum: CHART_DATUM,
};

// A height is a reading when it is a finite number, whatever its size: a minus
// tide below chart datum, or exactly -1 ft, is real. Null is the whole signal
// for none.
function heightOrNull(v: unknown): number | null {
  return typeof v === 'number' && Number.isFinite(v) ? v : null;
}

function normalise(data: Partial<TideToday>): TideToday {
  return {
    datetime: data.datetime || new Date().toISOString(),
    current_tide_height_ft: heightOrNull(data.current_tide_height_ft),
    tide_direction: data.tide_direction || '—',
    // A missing/unparseable time stays '' rather than becoming "now" -
    // substituting the current instant reads to the operator as a real
    // extreme (a fake "High · <now>"), which tideExtremesByTime/
    // nextTideExtreme (lib/tide-estimate.ts) and every renderer of this
    // field now treat as absent, not a real time to display.
    high_tide_time: typeof data.high_tide_time === 'string' ? data.high_tide_time : '',
    high_tide_height_ft: heightOrNull(data.high_tide_height_ft),
    low_tide_time: typeof data.low_tide_time === 'string' ? data.low_tide_time : '',
    low_tide_height_ft: heightOrNull(data.low_tide_height_ft),
    station_name: typeof data.station_name === 'string' ? data.station_name : '',
    provider: typeof data.provider === 'string' ? data.provider : '',
    height_datum: normaliseTideDatum(data.height_datum),
  };
}

declareQuery(['/api/tide-today'], { structuralSharing: true })

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
