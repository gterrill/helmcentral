import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import type { EngineProfile, EngineProfileProblem } from '@/lib/engine-profiles'

export function useEquipmentProfiles(enabled: boolean) {
  const [profiles, setProfiles] = useState<EngineProfile[]>([])
  const [problems, setProblems] = useState<EngineProfileProblem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reloadToken, setReloadToken] = useState(0)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false

    const load = async () => {
      setLoading(true)
      setError(null)
      try {
        const response = await fetch(`${apiBaseUrl}/api/equipment-profiles`)
        if (!response.ok) {
          // A 500 and an empty profiles directory must not render the same
          // way — the operator needs to know the fetch itself failed rather
          // than believing nothing is installed.
          const body = (await response.json().catch(() => null)) as { error?: unknown } | null
          const message = body && typeof body.error === 'string' ? body.error : `HTTP ${response.status}`
          if (!cancelled) {
            setError(message)
            setProfiles([])
            setProblems([])
          }
          return
        }
        const body = await response.json()
        if (cancelled) return
        setProfiles(Array.isArray(body?.profiles) ? body.profiles : [])
        setProblems(Array.isArray(body?.problems) ? body.problems : [])
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
          setProfiles([])
          setProblems([])
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    void load()
    return () => { cancelled = true }
  }, [enabled, reloadToken])

  const reload = () => setReloadToken((value) => value + 1)

  return { profiles, problems, loading, error, reload }
}
