import { useCallback, useEffect, useRef } from 'react'

/**
 * Hold-to-repeat for the Adjust mode's radius +/- buttons (and any other
 * step control that wants the same feel): a single tap steps once
 * immediately, and holding it down starts repeating after an initial delay,
 * then repeats faster than that delay so a long hold moves quickly without
 * needing a drag gesture. Pointer events (not mouse/touch separately) so one
 * set of handlers covers mouse, touch and pen.
 *
 * No timer is ever started merely by rendering this hook — only
 * onPointerDown does that — so it is StrictMode-safe: React's
 * mount/unmount/remount dance in development never starts a stray repeat
 * that outlives the component it belongs to.
 */
export const PRESS_REPEAT_INITIAL_DELAY_MS = 400
export const PRESS_REPEAT_INTERVAL_MS = 80

export interface PressRepeatHandlers {
  onPointerDown: () => void
  onPointerUp: () => void
  onPointerLeave: () => void
  onPointerCancel: () => void
}

export function usePressRepeat(onStep: () => void, disabled = false): PressRepeatHandlers {
  // Read fresh on every fire rather than closed over at onPointerDown time,
  // so a caller that re-renders with a new step size (e.g. imperial vs
  // metric) mid-hold uses the current callback, not the one captured when
  // the hold started.
  const onStepRef = useRef(onStep)
  onStepRef.current = onStep

  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const stop = useCallback(() => {
    if (timeoutRef.current !== null) {
      clearTimeout(timeoutRef.current)
      timeoutRef.current = null
    }
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current)
      intervalRef.current = null
    }
  }, [])

  const start = useCallback(() => {
    // A stray second pointerdown (e.g. a fast double-tap) restarts cleanly
    // rather than stacking a second interval on top of the first.
    stop()
    onStepRef.current()
    timeoutRef.current = setTimeout(() => {
      timeoutRef.current = null
      intervalRef.current = setInterval(() => onStepRef.current(), PRESS_REPEAT_INTERVAL_MS)
    }, PRESS_REPEAT_INITIAL_DELAY_MS)
  }, [stop])

  // Cleanup only — this effect starts nothing; it exists purely so a timer
  // left running by an unmount mid-hold doesn't fire into a gone component.
  useEffect(() => stop, [stop])

  // Code-review finding: holding + until the caller's own atMin/atMax makes
  // it disabled applies the `disabled` DOM attribute, which stops the
  // button from ever receiving the pointerup that would otherwise call
  // stop() — the repeat interval this hook started kept firing into a
  // control the operator can no longer even see respond. Watching disabled
  // directly cancels the repeat the instant the caller reports it, with no
  // dependency on a pointer event a disabled element will never dispatch.
  useEffect(() => {
    if (disabled) stop()
  }, [disabled, stop])

  return {
    onPointerDown: start,
    onPointerUp: stop,
    onPointerLeave: stop,
    onPointerCancel: stop,
  }
}
