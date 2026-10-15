import { useEffect, useRef, useState } from 'react'

import type { AppVersion } from '@/lib/build-stamp'
import { recordSeenVersion } from '@/lib/seen-version'

/**
 * The one place that decides whether to say "updated to vX": once the running
 * build is known, it is compared with the version this browser last showed
 * (see recordSeenVersion). Returns the version to announce, or null.
 *
 * `onAnnounce` runs in the same pass that recorded the version, so it fires
 * exactly once even though StrictMode runs the effect twice (the second pass
 * finds the version already recorded and says nothing).
 */
export function useVersionAnnouncement(
  current: AppVersion | null,
  onAnnounce?: (version: string) => void,
): string | null {
  const [announced, setAnnounced] = useState<string | null>(null)
  const onAnnounceRef = useRef(onAnnounce)
  onAnnounceRef.current = onAnnounce
  const version = current?.version ?? null

  useEffect(() => {
    if (version === null) return
    const to = recordSeenVersion(version)
    if (to === null) return
    setAnnounced(to)
    onAnnounceRef.current?.(to)
  }, [version])

  return announced
}
