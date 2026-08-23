import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import type { EngineProfile, EngineProfileProblem } from '@/lib/engine-profiles'

/**
 * Engine profiles are static drop-in files (ADR 0053), so this fetches once
 * when the dialog opens rather than polling — mirroring useSignalKPaths.
 *
 * `problems` carries files that failed to load. A bad drop-in file must not be
 * silent, so the dialog shows them.
 */
export function useEngineProfiles(enabled: boolean) {
  const [profiles, setProfiles] = useState<EngineProfile[]>([])
  const [problems, setProblems] = useState<EngineProfileProblem[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false

    const load = async () => {
      setLoading(true)
      try {
        const response = await fetch(`${apiBaseUrl}/api/engine-profiles`)
        const body = await response.json()
        if (cancelled) return
        setProfiles(Array.isArray(body?.profiles) ? body.profiles : [])
        setProblems(Array.isArray(body?.problems) ? body.problems : [])
      } catch {
        if (!cancelled) {
          setProfiles([])
          setProblems([])
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => { cancelled = true }
  }, [enabled])

  return { profiles, problems, loading }
}
