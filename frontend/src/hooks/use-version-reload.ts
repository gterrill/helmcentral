import { useEffect, useRef } from 'react'

import { apiBaseUrl } from '@/config/api'
import type { AppVersion } from '@/hooks/use-app-version'

export const VERSION_CHECK_INTERVAL_MS = 60 * 60 * 1000

export interface VersionReloadOptions {
  /** Clock, injectable for tests. */
  now?: () => number
  /** Reload action, injectable for tests. */
  reload?: () => void
  /** Return true while a reload would lose unsaved work; the reload waits for the next return to the page. */
  hasUnsavedWork?: () => boolean
}

async function readBuild(): Promise<AppVersion> {
  const response = await fetch(`${apiBaseUrl}/api/health`)
  if (!response.ok) throw new Error(`Health check failed: ${response.status}`)
  const data = (await response.json()) as Partial<AppVersion>
  if (!data.version) throw new Error('Health check returned no version')
  return { version: data.version, revision: data.revision ?? 'unknown' }
}

/**
 * Reloads the page once a newer Helmcentral build has been deployed, so a
 * phone or tablet left open never keeps running an old bundle. The build
 * stamp (`/api/health`) is read on load and remembered; when the page
 * regains attention (tab visible, window focus) it is read again, but at
 * most once an hour, with no timers. A failed or malformed read never
 * reloads and is not retried early: it is logged and the next eligible
 * return to the page tries again. If the first read failed, the next
 * successful one becomes the baseline instead of triggering a reload.
 */
export function useVersionReload(options: VersionReloadOptions = {}): void {
  const optionsRef = useRef(options)
  optionsRef.current = options

  useEffect(() => {
    const now = () => (optionsRef.current.now ?? Date.now)()
    let baseline: AppVersion | null = null
    let lastCheck = now()
    let checking = false
    let reloadWaiting = false
    let cancelled = false

    const reloadIfSafe = () => {
      if (optionsRef.current.hasUnsavedWork?.()) {
        reloadWaiting = true
        return
      }
      reloadWaiting = false
      ;(optionsRef.current.reload ?? (() => window.location.reload()))()
    }

    const check = async () => {
      checking = true
      lastCheck = now()
      try {
        const build = await readBuild()
        if (cancelled) return
        if (baseline === null) {
          baseline = build
        } else if (build.version !== baseline.version || build.revision !== baseline.revision) {
          reloadIfSafe()
        }
      } catch (err) {
        console.warn('Version check failed; will try again when the page next regains attention', err)
      } finally {
        checking = false
      }
    }

    const onAttention = () => {
      if (document.visibilityState === 'hidden') return
      if (reloadWaiting) {
        reloadIfSafe()
        return
      }
      if (checking || now() - lastCheck < VERSION_CHECK_INTERVAL_MS) return
      void check()
    }

    void check()
    document.addEventListener('visibilitychange', onAttention)
    window.addEventListener('focus', onAttention)
    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onAttention)
      window.removeEventListener('focus', onAttention)
    }
  }, [])
}
