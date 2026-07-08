import { useEffect, useRef, useState, useCallback } from 'react'

const AUTO_CLOSE_HYSTERESIS_SECONDS = 5
const DRAG_BUFFER_METERS = 4.572 // 15 feet

interface UseAnchorWatchAutoCloseResult {
  isAutoCloseArmed: boolean
  motoringSecondsElapsed: number
}

/**
 * Automatically closes anchor watch when engines start (navigationState becomes 'motoring')
 * AND the vessel drifts outside the anchor circle for 5+ seconds.
 *
 * This prevents users from forgetting to turn off the anchor alarm when underway.
 * Requires both conditions to be true for the hysteresis period before clearing.
 */
export function useAnchorWatchAutoClose(
  navigationState: string | null,
  distanceMeters: number | null,
  radiusMeters: number,
  anchorWatchActive: boolean,
  isEnabled: boolean,
): UseAnchorWatchAutoCloseResult {
  const motoringStartTimeRef = useRef<number | null>(null)
  const intervalIdRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const autoCloseInProgressRef = useRef(false)
  const [motoringSecondsElapsed, setMoturingSecondsElapsed] = useState(0)
  const [isAutoCloseArmed, setIsAutoCloseArmed] = useState(false)

  /**
   * Check if vessel is outside the anchor circle (within dragging threshold)
   */
  const isOutsideCircle = useCallback((): boolean => {
    if (distanceMeters === null) return false
    return distanceMeters > radiusMeters + DRAG_BUFFER_METERS
  }, [distanceMeters, radiusMeters])

  /**
   * Check if engines are running (motoring state)
   */
  const isMotoring = useCallback((): boolean => {
    return navigationState === 'motoring'
  }, [navigationState])

  /**
   * Perform the auto-close operation
   */
  const performAutoClose = useCallback(async () => {
    if (autoCloseInProgressRef.current) {
      return
    }

    autoCloseInProgressRef.current = true

    try {
      const response = await fetch('/api/anchor-watch', { method: 'DELETE' })
      if (response.ok) {
        console.log('[anchor-watch-auto-close] Anchor watch cleared automatically')

        // Dispatch custom event to trigger toast notification
        window.dispatchEvent(
          new CustomEvent('anchor-watch-auto-closed', {
            detail: { reason: 'engines_running' },
          }),
        )

        // Reset states
        motoringStartTimeRef.current = null
        setMoturingSecondsElapsed(0)
        setIsAutoCloseArmed(false)

        if (intervalIdRef.current) {
          clearInterval(intervalIdRef.current)
          intervalIdRef.current = null
        }

        autoCloseInProgressRef.current = false
      } else {
        console.error('[anchor-watch-auto-close] Failed to clear anchor watch:', response.statusText)
        autoCloseInProgressRef.current = false
      }
    } catch (err) {
      console.error('[anchor-watch-auto-close] Error clearing anchor watch:', err)
      autoCloseInProgressRef.current = false
    }
  }, [])

  /**
   * Main effect: monitor conditions and trigger auto-close
   */
  useEffect(() => {
    // Feature disabled or anchor watch not active
    if (!isEnabled || !anchorWatchActive) {
      motoringStartTimeRef.current = null
      autoCloseInProgressRef.current = false
      setMoturingSecondsElapsed(0)
      setIsAutoCloseArmed(false)

      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current)
        intervalIdRef.current = null
      }

      return
    }

    const motoring = isMotoring()
    const outside = isOutsideCircle()

    // Both conditions are true: start/continue countdown
    if (motoring && outside) {
      // First time entering this state
      if (motoringStartTimeRef.current === null) {
        motoringStartTimeRef.current = Date.now()
        setIsAutoCloseArmed(true)
        console.log('[anchor-watch-auto-close] Armed: engines running + outside circle')
      }

      // Clear any existing interval to avoid duplicates
      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current)
      }

      // Update elapsed time every 100ms for smooth countdown display
      intervalIdRef.current = setInterval(() => {
        if (motoringStartTimeRef.current === null) return

        const elapsedMs = Date.now() - motoringStartTimeRef.current
        const elapsedSec = Math.floor(elapsedMs / 1000)
        setMoturingSecondsElapsed(elapsedSec)

        // Fire auto-close when hysteresis period is met
        if (elapsedSec >= AUTO_CLOSE_HYSTERESIS_SECONDS) {
          void performAutoClose()
        }
      }, 100)
    } else {
      // One or both conditions went false: reset countdown
      if (motoringStartTimeRef.current !== null || isAutoCloseArmed) {
        motoringStartTimeRef.current = null
        autoCloseInProgressRef.current = false
        setMoturingSecondsElapsed(0)
        setIsAutoCloseArmed(false)

        if (motoring && !outside) {
          console.log('[anchor-watch-auto-close] Disarmed: engines running but inside circle')
        } else if (!motoring && outside) {
          console.log('[anchor-watch-auto-close] Disarmed: engines stopped, outside circle')
        } else {
          console.log('[anchor-watch-auto-close] Disarmed: engines stopped and back inside circle')
        }
      }

      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current)
        intervalIdRef.current = null
      }
    }

    // Cleanup on unmount
    return () => {
      if (intervalIdRef.current) {
        clearInterval(intervalIdRef.current)
      }
    }
  }, [isEnabled, anchorWatchActive, isMotoring, isOutsideCircle, performAutoClose, isAutoCloseArmed])

  return {
    isAutoCloseArmed,
    motoringSecondsElapsed,
  }
}
