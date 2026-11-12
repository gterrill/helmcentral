import { useEffect, useMemo, useState } from 'react';
import { declareQuery, useApiQuery } from '@/lib/api-query';
import { parseLightningWindow, type LightningFeed, type LightningWindow } from '@/lib/lightning-bands';

interface LightningEnvelopeApi {
  provider?: string;
  windows?: Array<{ start?: string; end?: string; probability?: number }>;
  updated_at?: string;
  ttl_seconds?: number;
  refreshing?: boolean;
  refresh_error?: string;
}

type MappedLightning = Omit<LightningFeed, 'providerSeen' | 'error'>;

/** While the host is fetching and holds nothing, the answer is empty; ask again soon. */
const REFRESHING_POLL_SECONDS = 10;
/** A refresh behind windows already drawn has nothing urgent to deliver. */
const REFRESHING_HELD_POLL_SECONDS = 120;
/** After a failed poll, whether or not windows are held: the host waits this long itself, so asking sooner only repeats the failure. */
const ERROR_POLL_SECONDS = 600;
/** Lightning follows the forecast runs, four a day. */
const DEFAULT_POLL_SECONDS = 21600;

/**
 * A window the page cannot read is dropped and logged rather than drawn as
 * some other value; the rest of the range still shows.
 */
function mapWindows(api: LightningEnvelopeApi['windows']): LightningWindow[] {
  if (!Array.isArray(api)) return [];
  const windows: LightningWindow[] = [];
  for (const raw of api) {
    try {
      windows.push(parseLightningWindow(raw ?? {}));
    } catch (err) {
      console.error('Dropping unreadable lightning window:', err);
    }
  }
  return windows;
}

function mapLightning(payload: LightningEnvelopeApi): MappedLightning {
  return {
    windows: mapWindows(payload.windows),
    provider: typeof payload.provider === 'string' && payload.provider !== '' ? payload.provider : null,
    refreshing: payload.refreshing === true,
    refreshError: typeof payload.refresh_error === 'string' && payload.refresh_error !== '' ? payload.refresh_error : null,
    updatedAt: typeof payload.updated_at === 'string' && payload.updated_at !== '' ? payload.updated_at : null,
    ttlSeconds: typeof payload.ttl_seconds === 'number' && payload.ttl_seconds > 0 ? payload.ttl_seconds : null,
  };
}

const EMPTY_LIGHTNING: MappedLightning = {
  windows: [],
  provider: null,
  refreshing: false,
  refreshError: null,
  updatedAt: null,
  ttlSeconds: null,
};

declareQuery(['/api/lightning'], { structuralSharing: true })

/**
 * Lightning probability at the boat, in 3-hour windows out to about 14.5 days.
 *
 * The answer is always for the boat's position: the host holds one grid cell
 * and takes no place. Like upper air it is optional, so a boat with no
 * lightning plugin gets an empty list and a 200. Only a configured provider
 * that has nothing held and is not refreshing answers an error.
 */
export function useLightning(): LightningFeed & { refetch: () => Promise<void> } {
  const [pollSeconds, setPollSeconds] = useState(DEFAULT_POLL_SECONDS);
  // Once any answer names a provider the page speaks about lightning, errors
  // included, for as long as it is open. A boat with no plugin never does.
  const [providerSeen, setProviderSeen] = useState(false);
  const { data, error, refetch } = useApiQuery<MappedLightning>({
    queryKey: ['/api/lightning'],
    path: '/api/lightning',
    refetchInterval: pollSeconds * 1000,
    select: mapLightning,
  });

  if (!providerSeen && data?.provider) setProviderSeen(true);

  useEffect(() => {
    if (error) console.error('Error fetching lightning forecast:', error);
  }, [error]);

  // The poll follows what the last answer said. The host answers a first
  // request at once with nothing and refreshes in the background, so the page
  // asks again soon rather than waiting out the TTL on an empty answer.
  useEffect(() => {
    // A new error wins over the previous answer api-query keeps beside it.
    if (error || data?.refreshError) setPollSeconds(ERROR_POLL_SECONDS);
    else if (data?.refreshing) setPollSeconds(data.windows.length === 0 ? REFRESHING_POLL_SECONDS : REFRESHING_HELD_POLL_SECONDS);
    else setPollSeconds(data?.ttlSeconds ?? DEFAULT_POLL_SECONDS);
  }, [data, error]);

  return useMemo(() => {
    const mapped = data ?? EMPTY_LIGHTNING;
    return {
      ...mapped,
      providerSeen: providerSeen || mapped.provider !== null,
      error: error ? error.message || 'Failed to load lightning forecast' : null,
      refetch,
    };
  }, [data, error, providerSeen, refetch]);
}
