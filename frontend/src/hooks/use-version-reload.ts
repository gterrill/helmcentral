import { useCallback, useEffect, useRef, useState } from 'react'

import { apiRequest } from '@/lib/api-query'
import type { AppVersion } from '@/hooks/use-app-version'

export const VERSION_CHECK_INTERVAL_MS = 5 * 60 * 1000
/** Focus and visibility returns check sooner than the timer, but not on every flick between windows. */
export const VERSION_ATTENTION_INTERVAL_MS = 60 * 1000
// A timer can fire a few ms ahead of the wall clock; it must not skip a whole interval for that.
const TIMER_TOLERANCE_MS = 5000

export type ChunkFailure = 'needs-update' | 'connection'

export interface VersionReloadOptions {
  /** Clock, injectable for tests. */
  now?: () => number
  /** Reload action, injectable for tests. */
  reload?: () => void
  /** True while a reload would lose unsaved work. */
  hasUnsavedWork?: () => boolean
  /**
   * True on a screen nobody is operating: the wall display and a Home Screen
   * dashboard. Only such a screen reloads by itself, and only with nothing
   * unsaved; every other screen is told an update is available and asked.
   */
  unattended?: () => boolean
}

export interface VersionWatch {
  /** The build this page was loaded as: the first successful read. Null until then. */
  current: AppVersion | null
  /** A newer build than `current`, once one has been seen. */
  available: AppVersion | null
  /** Set when a lazy chunk failed to load, and what the re-check made of it. */
  chunkFailure: ChunkFailure | null
  /** Checks again after a chunk failure; the outcome lands in `chunkFailure`. */
  recheckChunkFailure: () => void
}

async function readBuild(): Promise<AppVersion> {
  const data = await apiRequest<Partial<AppVersion>>('/api/health')
  if (!data.version) throw new Error('Health check returned no version')
  return { version: data.version, revision: data.revision ?? 'unknown' }
}

const differs = (a: AppVersion, b: AppVersion) => a.version !== b.version || a.revision !== b.revision

/**
 * Watches `/api/health` for a newer Helmcentral build than the one this page
 * loaded with. The first successful read is the baseline. Every five minutes
 * while the page is visible, and when it regains attention (at most once a
 * minute), the build is read again. A failed or malformed read never counts
 * as an update: it is logged and the next check tries again.
 *
 * The hook does not reload an attended screen. It reports `available` and
 * the caller asks the operator. A screen that is `unattended` reloads as soon
 * as an update is seen, unless there is unsaved work, in which case the
 * reload is retried after each later check.
 *
 * A lazy chunk that fails to load (`vite:preloadError`) triggers an
 * immediate re-check: a changed build means the chunk is gone because of a
 * deploy, anything else is reported as a connection problem.
 */
export function useVersionReload(options: VersionReloadOptions = {}): VersionWatch {
  const optionsRef = useRef(options)
  optionsRef.current = options

  const [current, setCurrent] = useState<AppVersion | null>(null)
  const [available, setAvailable] = useState<AppVersion | null>(null)
  const [chunkFailure, setChunkFailure] = useState<ChunkFailure | null>(null)
  const recheckRef = useRef<() => void>(() => {})

  useEffect(() => {
    const now = () => (optionsRef.current.now ?? Date.now)()
    let baseline: AppVersion | null = null
    let pending: AppVersion | null = null
    let lastCheck = now()
    let checking = false
    let cancelled = false

    const reloadIfUnattended = () => {
      const o = optionsRef.current
      if (pending === null || !o.unattended?.()) return
      if (o.hasUnsavedWork?.()) return
      ;(o.reload ?? (() => window.location.reload()))()
    }

    // Returns the build read, or null when the read failed.
    const observe = async (): Promise<AppVersion | null> => {
      try {
        const build = await readBuild()
        if (cancelled) return null
        if (baseline === null) {
          baseline = build
          setCurrent(build)
        } else if (differs(build, baseline)) {
          pending = build
          setAvailable(build)
        } else {
          pending = null
          setAvailable(null)
        }
        reloadIfUnattended()
        return build
      } catch (err) {
        console.warn('Version check failed; will try again at the next check', err)
        return null
      }
    }

    const check = async () => {
      checking = true
      lastCheck = now()
      try {
        await observe()
      } finally {
        checking = false
      }
    }

    const onAttention = (minimumMs: number) => {
      if (document.visibilityState === 'hidden') return
      if (checking || now() - lastCheck < minimumMs) return
      void check()
    }

    const onChunkFailure = async () => {
      if (cancelled) return
      const before = baseline
      const build = await observe()
      if (cancelled) return
      setChunkFailure(before !== null && build !== null && differs(build, before) ? 'needs-update' : 'connection')
    }

    const onPreloadError = (event: Event) => {
      // Handled here, so the failure is not swallowed silently: it is logged and surfaced.
      event.preventDefault()
      console.error('A screen failed to load', (event as Event & { payload?: unknown }).payload)
      void onChunkFailure()
    }

    recheckRef.current = () => { void onChunkFailure() }

    const onEvent = () => onAttention(VERSION_ATTENTION_INTERVAL_MS)
    void check()
    const timer = setInterval(() => onAttention(VERSION_CHECK_INTERVAL_MS - TIMER_TOLERANCE_MS), VERSION_CHECK_INTERVAL_MS)
    document.addEventListener('visibilitychange', onEvent)
    window.addEventListener('focus', onEvent)
    window.addEventListener('vite:preloadError', onPreloadError)
    return () => {
      cancelled = true
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onEvent)
      window.removeEventListener('focus', onEvent)
      window.removeEventListener('vite:preloadError', onPreloadError)
    }
  }, [])

  const recheckChunkFailure = useCallback(() => recheckRef.current(), [])
  return { current, available, chunkFailure, recheckChunkFailure }
}
