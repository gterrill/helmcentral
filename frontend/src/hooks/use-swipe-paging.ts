import { useEffect, useRef, useState } from 'react'

const MIN_DISTANCE_PX = 60
const MIN_DX_DY_RATIO = 1.5
const MAX_DURATION_MS = 600

// A surface that already gives horizontal drag its own meaning (chart
// scrubbing, a MapLibre map panning/zooming itself) or an ordinary text
// field the operator might be selecting in - a swipe starting inside one of
// these is a different gesture, not a page turn. `[data-no-swipe]` is the
// escape hatch for anything else that needs it (see tide-chart.tsx,
// forecast-drawer.tsx's hourly charts, and autopilot-tile.tsx's
// hold-to-confirm buttons, where a drifting hold must never double as a
// swipe).
const IGNORED_SURFACE_SELECTOR =
  '.maplibregl-map, .maplibregl-canvas, [data-no-swipe], input, textarea, select, [contenteditable]'

function gestureStartsInIgnoredArea(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(IGNORED_SURFACE_SELECTOR) !== null
}

export interface UseSwipePagingOptions {
  /** No listeners are attached at all while false (and any already attached
   *  are torn down) - the caller is expected to pass this only while a
   *  swipe should actually do something, e.g. the full screen dashboard
   *  with layout editing off. */
  enabled: boolean
  /** A left swipe (dx < 0, i.e. content moving toward the next page). */
  onNext: () => void
  /** A right swipe (dx > 0). */
  onPrevious: () => void
}

/**
 * Touch-only (pointerType 'touch'; mouse and pen are ignored, so a trackpad
 * or a stylus never turns the page) left/right swipe detection over the
 * given container.
 *
 * A gesture counts as a swipe once released, if: a single touch tracked it
 * start to finish (a second touch joining mid-gesture - a pinch - cancels
 * it), it covered at least 60px horizontally, that horizontal distance was
 * at least 1.5x whatever vertical distance came with it, and it took no
 * longer than 600ms. Everything else (a slower drag, a mostly-vertical one,
 * one that started on a surface with its own drag meaning) is left alone -
 * this hook never calls preventDefault, so nothing it declines to treat as
 * a swipe is disturbed.
 *
 * Returns a callback ref (a plain useState setter, not a RefObject),
 * because `enabled` is not the only thing that can change while the caller
 * keeps rendering: the element behind the ref can be swapped for a new one
 * with `enabled` staying true throughout — e.g. a session expiring while
 * full screen unmounts this surface for LoginScreen, and logging back in
 * mounts a fresh one with the dashboard already back in its full screen
 * state. A RefObject's `.current` would silently point at the detached old
 * node forever, since nothing about that swap would be in the effect's own
 * dependency array to make it re-run. Holding the element in state instead
 * means the effect below can depend on it directly and reattach its
 * listeners to whichever element is actually live.
 */
export function useSwipePaging<T extends HTMLElement>(options: UseSwipePagingOptions) {
  const [element, setElement] = useState<T | null>(null)

  // Read fresh on every fire rather than closed over at listener-attach
  // time (same reasoning as use-press-repeat's onStepRef), so a caller
  // whose onNext/onPrevious identity changes between renders - as App's do,
  // since they close over the current page index - never fires a stale one.
  const onNextRef = useRef(options.onNext)
  onNextRef.current = options.onNext
  const onPreviousRef = useRef(options.onPrevious)
  onPreviousRef.current = options.onPrevious

  useEffect(() => {
    if (!options.enabled) return
    const container = element
    if (!container) return

    // The pointerId currently being tracked as a candidate single-touch
    // swipe, or null once nothing is being tracked - either because nothing
    // has gone down yet, or because a second touch joining turned this into
    // a multi-touch gesture (a pinch), which cancels tracking outright
    // rather than letting the first touch's eventual pointerup still fire.
    let trackedPointerId: number | null = null
    let startX = 0
    let startY = 0
    let startTime = 0

    function handlePointerDown(event: PointerEvent) {
      if (event.pointerType !== 'touch') return
      if (trackedPointerId !== null) {
        // A second touch arriving mid-gesture means this was never a single-
        // finger swipe to begin with (a pinch, most likely) - abandon it
        // rather than let the first touch's pointerup still fire one.
        trackedPointerId = null
        return
      }
      if (gestureStartsInIgnoredArea(event.target)) return
      trackedPointerId = event.pointerId
      startX = event.clientX
      startY = event.clientY
      startTime = Date.now()
    }

    function handlePointerUp(event: PointerEvent) {
      if (trackedPointerId === null || event.pointerId !== trackedPointerId) return
      trackedPointerId = null

      const duration = Date.now() - startTime
      if (duration > MAX_DURATION_MS) return

      const dx = event.clientX - startX
      const dy = event.clientY - startY
      if (Math.abs(dx) < MIN_DISTANCE_PX) return
      if (Math.abs(dx) < MIN_DX_DY_RATIO * Math.abs(dy)) return

      if (dx < 0) onNextRef.current()
      else onPreviousRef.current()
    }

    function handlePointerCancel(event: PointerEvent) {
      if (event.pointerId === trackedPointerId) trackedPointerId = null
    }

    // Passive: this hook never calls preventDefault, so there is nothing for
    // the browser to wait on before scrolling, panning, or anything else a
    // touch might otherwise do.
    const listenerOptions: AddEventListenerOptions = { passive: true }
    // Without this the browser claims a sideways drag as its own pan and
    // fires pointercancel before the finger lifts, so pointerup never
    // arrives. pan-y keeps vertical scrolling; maps and horizontal scrollers
    // inside set their own touch-action and are unaffected.
    const previousTouchAction = container.style.touchAction
    container.style.touchAction = 'pan-y'
    container.addEventListener('pointerdown', handlePointerDown, listenerOptions)
    container.addEventListener('pointerup', handlePointerUp, listenerOptions)
    container.addEventListener('pointercancel', handlePointerCancel, listenerOptions)

    return () => {
      container.removeEventListener('pointerdown', handlePointerDown)
      container.removeEventListener('pointerup', handlePointerUp)
      container.removeEventListener('pointercancel', handlePointerCancel)
      container.style.touchAction = previousTouchAction
    }
  }, [options.enabled, element])

  return setElement
}
