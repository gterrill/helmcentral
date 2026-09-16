import { useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import type { EngineProfile, EngineProfileProblem } from '@/lib/engine-profiles'

export function useEquipmentProfiles(enabled: boolean) {
  const [profiles, setProfiles] = useState<EngineProfile[]>([])
  const [problems, setProblems] = useState<EngineProfileProblem[]>([])
  const [loading, setLoading] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false

    const load = async () => {
      setLoading(true)
      try {
        const response = await fetch(`${apiBaseUrl}/api/equipment-profiles`)
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
  }, [enabled, reloadToken])

  const reload = () => setReloadToken((value) => value + 1)

  return { profiles, problems, loading, reload }
}
