import { useCallback, useEffect, useState } from 'react'

import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'
import {
  emptyVesselCandidates,
  emptyVesselSettings,
  type VesselCandidatesResponse,
  type VesselSettings,
} from '@/lib/vessel-settings'

/**
 * Settings -> Vessel's own data layer: the vessel.* block itself
 * (GET/POST /api/vessel) plus the live candidates GET /api/vessel/candidates
 * lists engines/batteries from and reports each detector's setup status
 * from. Same "plain fetch-and-throw, one instance per caller" idiom as
 * use-inventory.ts/use-documents.ts (AGENTS.md fallback policy: no masking
 * fallback, the caller sees the real error).
 *
 * Saving re-fetches candidates afterward, not just settings: a save is what
 * changes each detector's "ready"/"missing" status (a linked profile just
 * got completed, an engine just got ticked), and the candidates response is
 * the only place that status lives.
 */
export function useVesselSettings() {
  const [settings, setSettings] = useState<VesselSettings>(emptyVesselSettings)
  const [candidates, setCandidates] = useState<VesselCandidatesResponse>(emptyVesselCandidates)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const [settingsRes, candidatesRes] = await Promise.all([
        fetch(`${apiBaseUrl}/api/vessel`),
        fetch(`${apiBaseUrl}/api/vessel/candidates`),
      ])
      if (!settingsRes.ok) throw new Error(await readErrorMessage(settingsRes))
      if (!candidatesRes.ok) throw new Error(await readErrorMessage(candidatesRes))
      const settingsBody = (await settingsRes.json()) as VesselSettings
      const candidatesBody = (await candidatesRes.json()) as VesselCandidatesResponse
      setSettings({
        engines: Array.isArray(settingsBody.engines) ? settingsBody.engines : [],
        house_bank: settingsBody.house_bank ?? null,
      })
      setCandidates({
        engines: Array.isArray(candidatesBody.engines) ? candidatesBody.engines : [],
        batteries: Array.isArray(candidatesBody.batteries) ? candidatesBody.batteries : [],
        detectors: candidatesBody.detectors ?? {},
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  /** Saves the whole vessel.* block (a full replace, matching POST /api/vessel's own contract) and re-fetches candidates. Throws on a rejected save, the same "no swallowed error" contract every other write in this codebase follows. */
  const save = useCallback(async (next: VesselSettings): Promise<void> => {
    setSaving(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/vessel`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(next),
      })
      if (!res.ok) throw new Error(await readErrorMessage(res))
      const saved = (await res.json()) as VesselSettings
      setSettings({
        engines: Array.isArray(saved.engines) ? saved.engines : [],
        house_bank: saved.house_bank ?? null,
      })
      await refresh()
    } finally {
      setSaving(false)
    }
  }, [refresh])

  return { settings, candidates, loading, error, saving, save, refresh }
}
