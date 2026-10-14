import { useEffect, useState } from 'react'
import { apiRequest } from '@/lib/api-query'

export interface AppVersion {
  version: string
  revision: string
}

export interface UseAppVersionResult {
  build: AppVersion | null
  loading: boolean
  error: string | null
}

// The probe lives at module level, not in the query store: the store drops an
// entry five minutes after its last reader leaves and the next mount would ask
// again, which could show a newly deployed version while this bundle still
// runs (useVersionReload decides when that bundle reloads). A failed probe is
// forgotten so the next mount tries again; a good one stands for the page load.
let probe: Promise<AppVersion> | null = null

function readBuild(): Promise<AppVersion> {
  if (probe === null) {
    const attempt = apiRequest<Partial<AppVersion>>('/api/health').then((data) => {
      if (!data.version) throw new Error('Health check returned no version')
      return { version: data.version, revision: data.revision ?? 'unknown' }
    })
    probe = attempt
    attempt.catch(() => {
      if (probe === attempt) probe = null
    })
  }
  return probe
}

/** For tests: forget the probe so the next mount reads again. */
export function resetAppVersionProbe(): void {
  probe = null
}

/**
 * Reads the build stamp off `/api/health`, the one place that knows what the
 * operator is actually running: the Dockerfile bakes the release tag and git
 * revision into the binary via ldflags, and `resolveBuildMetadata` falls back
 * to the Go build info when they weren't set. frontend/package.json's version
 * is not maintained against the release tags and must not be used for this.
 *
 * `/api/health` is a public-tier route (ADR 0040), so this works before login
 * as well as after. Probed once per page load — a build stamp cannot change
 * while the page is open; a new one implies a new backend and a fresh page
 * load.
 */
export function useAppVersion(): UseAppVersionResult {
  const [state, setState] = useState<UseAppVersionResult>({ build: null, loading: true, error: null })

  useEffect(() => {
    let cancelled = false
    readBuild().then(
      (build) => { if (!cancelled) setState({ build, loading: false, error: null }) },
      (err: unknown) => {
        if (!cancelled) setState({ build: null, loading: false, error: err instanceof Error ? err.message : String(err) })
      },
    )
    return () => { cancelled = true }
  }, [])

  return state
}
