import { useMemo } from 'react'
import { useApiQuery } from '@/lib/api-query'

export interface AppVersion {
  version: string
  revision: string
}

export interface UseAppVersionResult {
  build: AppVersion | null
  loading: boolean
  error: string | null
}

/**
 * Reads the build stamp off `/api/health`, the one place that knows what the
 * operator is actually running: the Dockerfile bakes the release tag and git
 * revision into the binary via ldflags, and `resolveBuildMetadata` falls back
 * to the Go build info when they weren't set. frontend/package.json's version
 * is not maintained against the release tags and must not be used for this.
 *
 * `/api/health` is a public-tier route (ADR 0040), so this works before login
 * as well as after. Probed once on mount — a build stamp cannot change while
 * the page is open; a new one implies a new backend and a fresh page load.
 */
export function useAppVersion(): UseAppVersionResult {
  const { data, isLoading, error } = useApiQuery<Partial<AppVersion>>({
    queryKey: ['/api/health'],
    path: '/api/health',
  })

  const build = useMemo(
    () => (data?.version ? { version: data.version, revision: data.revision ?? 'unknown' } : null),
    [data],
  )
  let message = error?.message ?? null
  if (!message && data && !data.version) message = 'Health check returned no version'
  return { build, loading: isLoading, error: message }
}
