import { useCallback, useEffect, useRef, useState } from 'react'

import { apiRequest } from '@/lib/api-query'
import { BUNDLE_BUILD, isUnstamped, type AppVersion } from '@/lib/build-stamp'
import { errorMessage } from '@/lib/errors'

export type { AppVersion }

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
  /** The build stamped into this bundle, injectable for tests. */
  bundle?: AppVersion
  /** True while a reload would lose unsaved work. */
  hasUnsavedWork?: () => boolean
  /**
   * True on a screen nobody is operating: the wall display and a Home Screen
   * dashboard with nothing open over it. Only such a screen reloads by
   * itself, and only with nothing unsaved; every other screen is told an
   * update is available and asked.
   */
  unattended?: () => boolean
}

export interface VersionWatch {
  /**
   * The build this page is running: the one stamped into the bundle. Only an
   * unstamped (dev) bundle takes it from the first health read instead, and
   * until that read answers it is null.
   */
  current: AppVersion | null
  /** Why `current` is still null, when the first health read failed. */
  readError: string | null
  /** A newer build than `current`, once one has been seen. */
  available: AppVersion | null
  /** Set when a lazy chunk failed to load, and what the re-check made of it. */
  chunkFailure: ChunkFailure | null
  /** A lazy chunk failed to load (reported by an error boundary): re-checks the build like a preload error does. */
  reportChunkFailure: (error: unknown) => void
  /** Resets `chunkFailure`, so the next failure is reported afresh. */
  clearChunkFailure: () => void
  /** Reloads now if an unattended screen is holding a reload that nothing now prevents. */
  retryHeldReload: () => void
}

async function readBuild(): Promise<AppVersion> {
  const data = await apiRequest<Partial<AppVersion>>('/api/health')
  if (!data.version) throw new Error('Health check returned no version')
  return { version: data.version, revision: data.revision ?? 'unknown' }
}

const differs = (a: AppVersion, b: AppVersion) => a.version !== b.version || a.revision !== b.revision

/**
 * Watches `/api/health` for a newer Helmcentral build than the one this page
 * was built as. The bundle's own stamp is the baseline; every five minutes
 * while the page is visible, and when it regains attention (at most once a
 * minute), the deployed build is read and compared. A failed or malformed
 * read never counts as an update: it is logged and the next check tries again.
 *
 * The hook does not reload an attended screen. It reports `available` and
 * the caller asks the operator. A screen that is `unattended` reloads as soon
 * as an update is seen, unless there is unsaved work, in which case the
 * reload is retried on every return to the page, on `retryHeldReload`, and
 * after each later check.
 *
 * A lazy chunk that fails to load (`vite:preloadError`, or an error boundary
 * reporting a rejected import) triggers an immediate re-check: a changed build
 * means the chunk is gone because of a deploy, anything else is reported as a
 * connection problem. The preload error itself is only logged and left alone,
 * so the import rejects and the boundary can show the failure in place.
 */
export function useVersionReload(options: VersionReloadOptions = {}): VersionWatch {
  const optionsRef = useRef(options)
  optionsRef.current = options
  const bundle = options.bundle ?? BUNDLE_BUILD
  const stamped = !isUnstamped(bundle)

  const [current, setCurrent] = useState<AppVersion | null>(stamped ? bundle : null)
  const [readError, setReadError] = useState<string | null>(null)
  const [available, setAvailable] = useState<AppVersion | null>(null)
  const [chunkFailure, setChunkFailure] = useState<ChunkFailure | null>(null)
  const reportRef = useRef<() => void>(() => {})
  const retryRef = useRef<() => void>(() => {})

  useEffect(() => {
    const now = () => (optionsRef.current.now ?? Date.now)()
    // A dev bundle has no stamp, so the first successful read stands in for it.
    // That is only for the dev stack, where the bundle and backend are always
    // the same checkout; a release bundle never takes this path.
    let baseline: AppVersion | null = stamped ? bundle : null
    let pending: AppVersion | null = null
    let lastCheck = now()
    let checking = false
    let rechecking = false
    let cancelled = false
    let reloading = false

    const reloadIfUnattended = () => {
      const o = optionsRef.current
      if (reloading || pending === null || !o.unattended?.()) return
      if (o.hasUnsavedWork?.()) return
      reloading = true
      ;(o.reload ?? (() => window.location.reload()))()
    }

    // Returns the build read, or null when the read failed.
    const observe = async (): Promise<AppVersion | null> => {
      try {
        const build = await readBuild()
        if (cancelled) return null
        setReadError(null)
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
        if (!cancelled && baseline === null) setReadError(errorMessage(err))
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
      if (cancelled || rechecking) return
      rechecking = true
      try {
        const before = baseline
        const build = await observe()
        if (cancelled) return
        setChunkFailure(before !== null && build !== null && differs(build, before) ? 'needs-update' : 'connection')
      } finally {
        rechecking = false
      }
    }

    const onPreloadError = (event: Event) => {
      // Only logged. Left alone, the import rejects and the error boundary
      // around the lazy screens shows it; preventDefault would make Vite
      // resolve the import to undefined and the lazy wrapper throw a TypeError.
      console.error('A screen failed to load', (event as Event & { payload?: unknown }).payload)
      void onChunkFailure()
    }

    reportRef.current = () => { void onChunkFailure() }
    retryRef.current = reloadIfUnattended

    // A reload held for unsaved work needs no health read to be retried.
    const onEvent = () => {
      if (document.visibilityState !== 'hidden') reloadIfUnattended()
      onAttention(VERSION_ATTENTION_INTERVAL_MS)
    }
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
    // The stamp is fixed for the life of the page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const reportChunkFailure = useCallback((error: unknown) => {
    console.error('A screen failed to load', error)
    reportRef.current()
  }, [])
  const clearChunkFailure = useCallback(() => setChunkFailure(null), [])
  const retryHeldReload = useCallback(() => retryRef.current(), [])
  return { current, readError, available, chunkFailure, reportChunkFailure, clearChunkFailure, retryHeldReload }
}
