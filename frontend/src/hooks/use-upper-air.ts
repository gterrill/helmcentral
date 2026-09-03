import { useCallback, useEffect, useRef, useState } from 'react';

/**
 * The 500mb outlook for one forecast day.
 *
 * Every figure is relative to the rest of the forecast window at this
 * position rather than to any absolute threshold. Surviving the Storm, which
 * these signals come from, gives no numeric 500mb values: its method is
 * reading successive charts. A percentile is the honest way to say "this is
 * the low end of the coming fortnight" without inventing a constant, and it
 * needs no retuning as the boat moves between latitudes.
 */
export interface UpperAirOutlook {
  /** false when no provider is installed, or the provider had no data for this day. */
  present: boolean;
  height500M: number;
  /** 500mb minus 1000mb height. Low thickness over warm water means cold air aloft. */
  thicknessM: number;
  peakWind500Kts: number;
  /** Where this day's height sits in the window, 0 being the lowest. */
  heightPercentile: number;
  tendency24hM: number;
  /**
   * The day sits at the low end of the window after a real fall, so
   * conditions aloft support a surface low developing.
   */
  troughSupport: boolean;
}

/**
 * One sample of the sub-daily 500mb trace.
 *
 * The day cards carry a daily mean, which is the right figure for a badge and
 * the wrong one for a chart: a trough drawn at one point per day is a sawtooth,
 * and the fall into it is the part the book actually reads.
 */
export interface UpperAirSample {
  time: string;
  dayKey: string;
  /**
   * Hour of the vessel's local day. Carried from the backend rather than
   * recovered from `time` in the browser, whose timezone is not the vessel's
   * and would produce a label that disagrees with `dayKey`.
   */
  localHour: number;
  height500M: number;
  /** 500mb minus 1000mb height. Low thickness over warm water means cold air aloft. */
  thicknessM: number;
  wind500Kts: number;
  temperature500C: number;
}

/**
 * The vertical extent of the forecast window, so the chart can draw the range
 * each day is judged against rather than restating a percentile in prose.
 *
 * `lowQuintileM` is the same edge `troughSupport` tests, computed once on the
 * backend off the same sorted list, so the drawn band and the marked days
 * cannot disagree.
 */
export interface UpperAirWindow {
  present: boolean;
  lowM: number;
  highM: number;
  lowQuintileM: number;
}

export interface UpperAirDay {
  dayKey: string;
  date: string;
  dayName: string;
  outlook: UpperAirOutlook;
}

interface UpperAirOutlookApi {
  present?: boolean;
  height_500_m?: number;
  thickness_m?: number;
  peak_wind_500_kts?: number;
  height_percentile?: number;
  tendency_24h_m?: number;
  trough_support?: boolean;
}

interface UpperAirDayApi {
  day_key?: string;
  date?: string;
  day_name?: string;
  outlook?: UpperAirOutlookApi;
}

interface UpperAirSampleApi {
  time?: string;
  day_key?: string;
  local_hour?: number;
  height_500_m?: number;
  thickness_m?: number;
  wind_500_kts?: number;
  temperature_500_c?: number;
}

interface UpperAirWindowApi {
  present?: boolean;
  low_m?: number;
  high_m?: number;
  low_quintile_m?: number;
}

interface UpperAirEnvelopeApi {
  provider?: string;
  days?: UpperAirDayApi[];
  series?: UpperAirSampleApi[];
  window?: UpperAirWindowApi;
  cached?: boolean;
  updated_at?: string;
  ttl_seconds?: number;
}

const ABSENT: UpperAirOutlook = {
  present: false,
  height500M: 0,
  thicknessM: 0,
  peakWind500Kts: 0,
  heightPercentile: 0,
  tendency24hM: 0,
  troughSupport: false,
};

const ABSENT_WINDOW: UpperAirWindow = { present: false, lowM: 0, highM: 0, lowQuintileM: 0 };

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function mapWindow(api: UpperAirWindowApi | undefined): UpperAirWindow {
  if (!api || api.present !== true) {
    return ABSENT_WINDOW;
  }
  return {
    present: true,
    lowM: num(api.low_m),
    highM: num(api.high_m),
    lowQuintileM: num(api.low_quintile_m),
  };
}

/**
 * A sample without a 500mb height is not a reading of sea level, it is a gap,
 * and dropping it here keeps it from drawing as a spike to the floor of the
 * chart.
 */
function mapSeries(api: UpperAirSampleApi[] | undefined): UpperAirSample[] {
  if (!Array.isArray(api)) {
    return [];
  }
  return api
    .filter((sample) => typeof sample?.time === 'string' && num(sample.height_500_m) > 0)
    .map((sample) => ({
      time: sample.time as string,
      dayKey: typeof sample.day_key === 'string' ? sample.day_key : '',
      localHour: num(sample.local_hour),
      height500M: num(sample.height_500_m),
      thicknessM: num(sample.thickness_m),
      wind500Kts: num(sample.wind_500_kts),
      temperature500C: num(sample.temperature_500_c),
    }));
}

function mapOutlook(api: UpperAirOutlookApi | undefined): UpperAirOutlook {
  if (!api || api.present !== true) {
    return ABSENT;
  }
  return {
    present: true,
    height500M: typeof api.height_500_m === 'number' ? api.height_500_m : 0,
    thicknessM: typeof api.thickness_m === 'number' ? api.thickness_m : 0,
    peakWind500Kts: typeof api.peak_wind_500_kts === 'number' ? api.peak_wind_500_kts : 0,
    heightPercentile: typeof api.height_percentile === 'number' ? api.height_percentile : 0,
    tendency24hM: typeof api.tendency_24h_m === 'number' ? api.tendency_24h_m : 0,
    troughSupport: api.trough_support === true,
  };
}

/**
 * Upper air is optional: a boat with no upper-air plugin installed gets an
 * empty day list and a 200, not an error, because that is a normal
 * configuration rather than a fault. Only a configured-but-broken provider
 * sets `error`.
 */
export function useUpperAir(refreshIntervalSeconds = 21600) {
  const [days, setDays] = useState<UpperAirDay[]>([]);
  const [series, setSeries] = useState<UpperAirSample[]>([]);
  const [windowBand, setWindowBand] = useState<UpperAirWindow>(ABSENT_WINDOW);
  const [provider, setProvider] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const hasLoadedDataRef = useRef(false);

  const fetchUpperAir = useCallback(async () => {
    try {
      if (!hasLoadedDataRef.current) {
        setLoading(true);
      }

      const response = await fetch('/api/upper-air');
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const payload = (await response.json()) as UpperAirEnvelopeApi;
      const rawDays = Array.isArray(payload.days) ? payload.days : [];

      setDays(
        rawDays.map((day) => ({
          dayKey: typeof day.day_key === 'string' ? day.day_key : '',
          date: typeof day.date === 'string' ? day.date : '',
          dayName: typeof day.day_name === 'string' ? day.day_name : '',
          outlook: mapOutlook(day.outlook),
        })),
      );
      setSeries(mapSeries(payload.series));
      setWindowBand(mapWindow(payload.window));
      setProvider(typeof payload.provider === 'string' && payload.provider !== '' ? payload.provider : null);
      if (rawDays.length > 0) {
        hasLoadedDataRef.current = true;
      }
      setError(null);
    } catch (err) {
      console.error('Error fetching upper-air forecast:', err);
      setError(err instanceof Error ? err.message : 'Failed to load upper-air forecast');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchUpperAir();
    const interval = setInterval(fetchUpperAir, refreshIntervalSeconds * 1000);
    return () => clearInterval(interval);
  }, [fetchUpperAir, refreshIntervalSeconds]);

  return { days, series, windowBand, provider, loading, error, refetch: fetchUpperAir };
}
