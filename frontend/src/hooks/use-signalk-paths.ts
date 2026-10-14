import { useApiQuery } from '@/lib/api-query'

export interface SignalKPath {
  path: string
  units?: string
  value?: number
}

const NONE: SignalKPath[] = []

/**
 * Every path the SignalK server is currently publishing, for the gauge path
 * picker. Fetched on demand rather than streamed: it only matters while the
 * config dialog is open.
 */
export function useSignalKPaths(enabled: boolean) {
  const { data } = useApiQuery<SignalKPath[]>({
    queryKey: ['/api/signalk/paths'],
    path: '/api/signalk/paths',
    enabled,
    // The picker falls back to free text, so a failure here is survivable and
    // is not shown.
    select: (raw: { paths?: SignalKPath[] }) => (Array.isArray(raw.paths) ? raw.paths : NONE),
  })
  return { paths: data ?? NONE }
}
