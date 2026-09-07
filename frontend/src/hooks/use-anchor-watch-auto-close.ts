import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { anchorRequest } from '@/lib/anchor-request'

const AUTO_CLOSE_HYSTERESIS_SECONDS = 5
const DRAG_BUFFER_METERS = 4.572

/** Departure depends on engine telemetry, not Auto-state's anchored latch.
 * The caller withholds distance when GNSS is critical. Still browser-driven:
 * server-side drag detection remains active when the dashboard is closed. */
export function useAnchorWatchAutoClose(
  engine0Rpm: number | null | undefined,
  engine1Rpm: number | null | undefined,
  distanceMeters: number | null,
  radiusMeters: number,
  anchorWatchActive: boolean,
  isEnabled: boolean,
) {
  const startRef = useRef<number | null>(null)
  const inFlightRef = useRef(false)
  const attemptedRef = useRef(false)
  const completedRef = useRef(false)
  const [motoringSecondsElapsed, setElapsed] = useState(0)
  const [isAutoCloseArmed, setArmed] = useState(false)
  const enginesRunning = [engine0Rpm, engine1Rpm].some(
    (rpm) => typeof rpm === 'number' && Number.isFinite(rpm) && rpm > 0,
  )
  const eligible = isEnabled && anchorWatchActive && enginesRunning
    && distanceMeters !== null && Number.isFinite(distanceMeters)
    && Number.isFinite(radiusMeters) && radiusMeters > 0
    && distanceMeters > radiusMeters + DRAG_BUFFER_METERS

  useEffect(() => {
    if (!anchorWatchActive) completedRef.current = false
    if (!eligible) {
      startRef.current = null
      attemptedRef.current = false
      setElapsed(0)
      setArmed(false)
      return
    }
    if (completedRef.current || attemptedRef.current || inFlightRef.current) return

    startRef.current = Date.now()
    setArmed(true)
    let disposed = false
    const interval = setInterval(() => {
      if (startRef.current === null || inFlightRef.current || attemptedRef.current) return
      const elapsed = Math.floor((Date.now() - startRef.current) / 1000)
      setElapsed(elapsed)
      if (elapsed < AUTO_CLOSE_HYSTERESIS_SECONDS) return
      clearInterval(interval)
      attemptedRef.current = true
      inFlightRef.current = true
      setArmed(false)
      void anchorRequest({ method: 'DELETE' }).then(() => {
        completedRef.current = true
        window.dispatchEvent(new CustomEvent('anchor-watch-auto-closed', {
          detail: { reason: 'engines_running' },
        }))
      }).catch((error: unknown) => {
        toast.error('Could not automatically raise anchor', {
          description: error instanceof Error ? error.message : 'Request failed',
        })
      }).finally(() => {
        inFlightRef.current = false
        startRef.current = null
        if (!disposed) setElapsed(0)
      })
    }, 100)
    return () => {
      disposed = true
      clearInterval(interval)
    }
  }, [eligible, anchorWatchActive])

  return { isAutoCloseArmed, motoringSecondsElapsed }
}