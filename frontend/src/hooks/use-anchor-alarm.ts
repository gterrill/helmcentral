import { useEffect, useRef, useCallback, useState } from 'react'
import type { AnchorWatchState } from './use-anchor-watch'

interface UseAnchorAlarmResult {
  isAlarming: boolean
  isSilenced: boolean
  silence: () => void
}

/**
 * Manages anchor watch alarm audio playback and screen wake lock.
 * 
 * - Plays a looping klaxon when anchorState transitions into 'dragging'
 * - Keeps alarm silent once user calls silence(), resets when state leaves alarm zone
 * - Requests screen wake lock when alarm is active (prevents tablet sleep)
 * - Handles browser autoplay policy by lazy-initializing AudioContext
 */
export function useAnchorAlarm(anchorState: AnchorWatchState): UseAnchorAlarmResult {
  const audioContextRef = useRef<AudioContext | null>(null)
  const oscillatorsRef = useRef<OscillatorNode[]>([])
  const gainsRef = useRef<GainNode[]>([])
  const isPlayingRef = useRef(false)
  const prevStateRef = useRef<AnchorWatchState>('none')
  const [isSilenced, setIsSilenced] = useState(false)
  const [isAlarming, setIsAlarming] = useState(false)
  const wakeLockRef = useRef<WakeLockSentinel | null>(null)

  /**
   * Initialize AudioContext on first user gesture (lazy-load pattern)
   * Required by browser autoplay policy
   */
  const ensureAudioContext = useCallback(async () => {
    if (audioContextRef.current) return

    try {
      const ctx = new (window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext)()
      audioContextRef.current = ctx

      // If context is suspended (browser policy), log but don't throw
      if (ctx.state === 'suspended') {
        console.log('[anchor-alarm] AudioContext suspended; will unlock on next interaction')
      }
    } catch (err) {
      console.error('[anchor-alarm] Failed to initialize AudioContext:', err)
    }
  }, [])

  /**
   * Resume AudioContext if suspended (autoplay policy)
   */
  const resumeAudioContext = useCallback(async () => {
    if (!audioContextRef.current || audioContextRef.current.state !== 'suspended') return

    try {
      await audioContextRef.current.resume()
      console.log('[anchor-alarm] AudioContext resumed')
    } catch (err) {
      console.error('[anchor-alarm] Failed to resume AudioContext:', err)
    }
  }, [])

  /**
   * Stop all active oscillators
   */
  const stopKlaxon = useCallback(() => {
    oscillatorsRef.current.forEach((osc) => {
      try {
        osc.stop()
      } catch {
        // Already stopped, ignore
      }
    })
    oscillatorsRef.current = []
    gainsRef.current = []
    isPlayingRef.current = false
  }, [])

  /**
   * Generate a ship's klaxon alarm: two square-wave tones alternating
   * ~500Hz primary, ~400Hz secondary, ~80 BPM double-beep pattern
   */
  const playKlaxon = useCallback(async () => {
    if (!audioContextRef.current) {
      await ensureAudioContext()
      if (!audioContextRef.current) return
    }

    const ctx = audioContextRef.current
    await resumeAudioContext()

    // Stop any existing oscillators
    stopKlaxon()

    const now = ctx.currentTime
    const masterGain = ctx.createGain()
    masterGain.connect(ctx.destination)
    masterGain.gain.setValueAtTime(0.3, now) // ~60% volume to avoid startling

    // Pattern: beep-silence-beep-silence, repeat ~every 800ms
    const beepDuration = 0.15 // 150ms per beep
    const silenceDuration = 0.1 // 100ms silence
    const cycleDuration = (beepDuration + silenceDuration) * 2 // total cycle time

    // Create two square-wave tones for richer alarm
    const freq1 = 500
    const freq2 = 400

    // Schedule 20 cycles (~16 seconds) of klaxon per play call
    // Loop will restart via recursive scheduling in the effect
    for (let cycle = 0; cycle < 20; cycle++) {
      const cycleStart = now + cycle * cycleDuration

      // First beep: freq1
      const osc1 = ctx.createOscillator()
      const gain1 = ctx.createGain()
      osc1.type = 'square'
      osc1.frequency.setValueAtTime(freq1, cycleStart)
      osc1.connect(gain1)
      gain1.connect(masterGain)
      gain1.gain.setValueAtTime(0.7, cycleStart)
      gain1.gain.setValueAtTime(0, cycleStart + beepDuration)
      osc1.start(cycleStart)
      osc1.stop(cycleStart + beepDuration)
      oscillatorsRef.current.push(osc1)
      gainsRef.current.push(gain1)

      // Silence gap
      // (implicit via gain envelope above)

      // Second beep: freq2
      const osc2 = ctx.createOscillator()
      const gain2 = ctx.createGain()
      osc2.type = 'square'
      osc2.frequency.setValueAtTime(freq2, cycleStart + beepDuration + silenceDuration)
      osc2.connect(gain2)
      gain2.connect(masterGain)
      gain2.gain.setValueAtTime(0.7, cycleStart + beepDuration + silenceDuration)
      gain2.gain.setValueAtTime(0, cycleStart + beepDuration + silenceDuration + beepDuration)
      osc2.start(cycleStart + beepDuration + silenceDuration)
      osc2.stop(cycleStart + beepDuration + silenceDuration + beepDuration)
      oscillatorsRef.current.push(osc2)
      gainsRef.current.push(gain2)
    }

    isPlayingRef.current = true
  }, [ensureAudioContext, resumeAudioContext, stopKlaxon])

  /**
   * Request screen wake lock to prevent tablet sleep while alarm is active
   */
  const requestWakeLock = useCallback(async () => {
    if (wakeLockRef.current) return // Already locked

    try {
      if ('wakeLock' in navigator) {
        wakeLockRef.current = await navigator.wakeLock.request('screen')
        console.log('[anchor-alarm] Screen wake lock acquired')
      } else {
        console.warn('[anchor-alarm] Screen wake lock not supported on this device')
      }
    } catch (err) {
      console.error('[anchor-alarm] Failed to acquire screen wake lock:', err)
    }
  }, [])

  /**
   * Release screen wake lock
   */
  const releaseWakeLock = useCallback(async () => {
    if (!wakeLockRef.current) return

    try {
      await wakeLockRef.current.release()
      wakeLockRef.current = null
      console.log('[anchor-alarm] Screen wake lock released')
    } catch (err) {
      console.error('[anchor-alarm] Failed to release screen wake lock:', err)
    }
  }, [])

  /**
   * Silence the alarm until state exits and re-enters alarm zone
   */
  const silence = useCallback(() => {
    setIsSilenced(true)
    stopKlaxon()
  }, [stopKlaxon])

  /**
   * Main effect: monitor anchorState and control alarm playback
   */
  useEffect(() => {
    const isInAlarmState = anchorState === 'dragging'
    const wasInAlarmState = prevStateRef.current === 'dragging'

    // State transition into alarm zone
    if (isInAlarmState && !wasInAlarmState) {
      setIsAlarming(true)
      setIsSilenced(false) // Reset silence flag on new alarm event
      requestWakeLock()

      if (!isSilenced) {
        playKlaxon()
        // Schedule recurring playback to loop indefinitely
        const interval = setInterval(() => {
          if (!isPlayingRef.current && isInAlarmState && !isSilenced) {
            playKlaxon()
          }
        }, 16000) // Restart every 16 seconds (20 cycles × 800ms per cycle)

        prevStateRef.current = anchorState
        return () => clearInterval(interval)
      }
    }

    // State transition out of alarm zone
    if (!isInAlarmState && wasInAlarmState) {
      setIsAlarming(false)
      stopKlaxon()
      releaseWakeLock()
    }

    // If alarming but isSilenced changed, stop playback
    if (isInAlarmState && isSilenced && isPlayingRef.current) {
      stopKlaxon()
    }

    prevStateRef.current = anchorState

    return () => {
      // Cleanup on unmount
      stopKlaxon()
      releaseWakeLock()
    }
  }, [anchorState, isSilenced, playKlaxon, stopKlaxon, requestWakeLock, releaseWakeLock])

  return { isAlarming, isSilenced, silence }
}
