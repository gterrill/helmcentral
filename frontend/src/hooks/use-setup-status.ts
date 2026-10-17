import { getQueryData, useApiQuery } from '@/lib/api-query'
import { emptySetupStatus, type SetupStatus } from '@/lib/setup-status'

const KEY = ['/api/setup/status']

/**
 * How far first-run setup has got (GET /api/setup/status): SignalK connected,
 * engines, house bank, charging and power sources. Computed on the server from
 * live state on every read, so a step that becomes undone comes back by itself.
 * Re-read every minute while setup is unfinished; once complete, every 5 minutes.
 * Saves and equipment changes invalidate it, so a step that becomes undone shows
 * at once.
 *
 * `loaded` is false until the first answer, so a caller can stay quiet rather
 * than draw "Setup 0 of 0".
 */
export function useSetupStatus(enabled = true) {
  const complete = getQueryData<SetupStatus>(KEY)?.complete === true
  const { data, isLoading, error, refetch } = useApiQuery<SetupStatus>({
    queryKey: KEY,
    path: '/api/setup/status',
    enabled,
    refetchInterval: complete ? 300_000 : 60_000,
    select: (body: SetupStatus) => ({
      steps: Array.isArray(body.steps) ? body.steps : [],
      complete: body.complete === true,
    }),
  })
  return {
    status: data ?? emptySetupStatus,
    loaded: data !== undefined,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
  }
}
