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

interface UpperAirEnvelopeApi {
  provider?: string;
  days?: UpperAirDayApi[];
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

  return { days, provider, loading, error, refetch: fetchUpperAir };
}
