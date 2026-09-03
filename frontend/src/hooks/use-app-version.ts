import { useEffect, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

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
  const [build, setBuild] = useState<AppVersion | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    const fetchVersion = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/health`)
        if (!response.ok) {
          throw new Error(`Health check failed: ${response.status}`)
        }
        const data = (await response.json()) as Partial<AppVersion>
        if (cancelled) return
        if (!data.version) {
          throw new Error('Health check returned no version')
        }
        setBuild({ version: data.version, revision: data.revision ?? 'unknown' })
      } catch (err) {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Unable to read build version')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void fetchVersion()

    return () => {
      cancelled = true
    }
  }, [])

  return { build, loading, error }
}
