import { useEffect } from 'react'

import { clearPendingSetupFocus, focusSetupControl, peekPendingSetupFocus, subscribeSetupFocus } from '@/lib/setup-status'

/**
 * Settings sections call this to act on a pending setup focus request (the
 * dashboard line, a tile or the setup checklist asked for one control). Parts
 * of a section draw a moment after it, so it keeps trying for a few seconds
 * before giving up. A request made while the section is already showing is
 * acted on too. The request is peeked, never taken: StrictMode runs the effect
 * twice, and it is cleared only once met or given up on.
 *
 * `accepts` limits the section to the controls it owns, so a request meant
 * for another section is left alone.
 */
export function useSetupFocus(accepts?: (control: string) => boolean): void {
  useEffect(() => {
    let frame = 0
    const run = () => {
      cancelAnimationFrame(frame)
      const control = peekPendingSetupFocus()
      if (control === null || (accepts && !accepts(control))) return
      let tries = 0
      const attempt = () => {
        if (focusSetupControl(control)) {
          clearPendingSetupFocus()
          return
        }
        if (++tries >= 300 || peekPendingSetupFocus() !== control) {
          if (peekPendingSetupFocus() === control) clearPendingSetupFocus()
          return
        }
        frame = requestAnimationFrame(attempt)
      }
      frame = requestAnimationFrame(attempt)
    }
    run()
    const unsubscribe = subscribeSetupFocus(run)
    return () => {
      unsubscribe()
      cancelAnimationFrame(frame)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- accepts is a stable module-level predicate
  }, [])
}
