import { useEffect, useRef, useState } from 'react'

export type ScreenWakeLockStatus = 'off' | 'held' | 'unsupported' | 'denied'

export interface ScreenWakeLockState {
  status: ScreenWakeLockStatus
}

/**
 * Best-effort screen wake lock for a wall display (ADR 0110 §5c). Per
 * AGENTS.md's fallback policy, a best-effort feature has to report when it
 * isn't working rather than silently doing nothing — hence the returned
 * status, which DisplayStatusBadge shows for anything other than
 * `'off'`/`'held'` rather than only ever showing a lock icon that may or may
 * not mean anything.
 *
 * The Screen Wake Lock spec releases the lock automatically the instant the
 * document is hidden (an app switch, the browser's own chrome stealing
 * focus), so a single request made on mount cannot be trusted to still hold
 * hours later — this re-requests it every time the document becomes visible
 * again rather than assuming it does.
 */
export function useScreenWakeLock(enabled: boolean): ScreenWakeLockState {
  const [status, setStatus] = useState<ScreenWakeLockStatus>('off')
  const sentinelRef = useRef<WakeLockSentinel | null>(null)

  useEffect(() => {
    if (!enabled) {
      setStatus('off')
      return
    }

    if (typeof navigator === 'undefined' || !('wakeLock' in navigator)) {
      setStatus('unsupported')
      return
    }

    let cancelled = false

    async function acquire() {
      try {
        const sentinel = await navigator.wakeLock.request('screen')
        if (cancelled) {
          void sentinel.release()
          return
        }
        sentinelRef.current = sentinel
        setStatus('held')
      } catch {
        if (!cancelled) setStatus('denied')
      }
    }

    function handleVisibilityChange() {
      if (document.visibilityState === 'visible') void acquire()
    }

    void acquire()
    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      const held = sentinelRef.current
      sentinelRef.current = null
      void held?.release()
    }
  }, [enabled])

  return { status }
}
