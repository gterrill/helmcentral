import { useEffect, useRef, useCallback, useState } from 'react'
import type { ActiveAlarm } from './use-alarms'

interface UseAnchorAlarmResult {
  /** Audible: a live drag, not yet silenced (or force-resumed via
   *  `unsilence`). The klaxon sounds while this is true. */
  isAlarming: boolean
  /** A live drag the server has acknowledged, and this device has not
   *  locally overridden back to audible. Distinct from "no alarm at all" —
   *  see `isActive`. */
  isSilenced: boolean
  /** Whether there is a live anchor-drag condition at all, silenced or not.
   *  Without this, a caller has only isAlarming/isSilenced to go on, and it
   *  is easy to mistake "silenced" for "nothing to show" — which is exactly
   *  the P0 bug this hook's callers must not repeat. */
  isActive: boolean
  silence: () => void
  /**
   * Resumes the local klaxon for a drag the server has already
   * acknowledged. There is no server-side "un-acknowledge" — SignalK's own
   * notification model and this engine both treat acknowledged as terminal
   * until the condition clears and re-raises (ADR 0038) — so this is a
   * same-device override of the local audio only, not a state change every
   * screen picks up. Calling it while there is nothing acknowledged to
   * resume is a no-op.
   */
  unsilence: () => void
}

/**
 * Plays the local klaxon for a server-detected anchor drag, and keeps the
 * screen awake while it sounds.
 *
 * Drag detection itself is server-side (ADR 0038). This hook renders the audible
 * half only: when it lived here, closing the tab silenced the alarm, which is
 * the worst property an anchor alarm can have. The server now also logs it and
 * delivers it off the boat.
 *
 * Silencing is likewise the server's acknowledgement, not local state, so every
 * screen agrees and a second browser cannot be left ringing. That also removes
 * the bug in the previous version: the re-scheduling interval was created only
 * on the transition into 'dragging', so after un-silencing it was never
 * recreated and the klaxon stayed quiet for the rest of the drag.
 */
export function useAnchorAlarm(
  alarm: ActiveAlarm | null,
  acknowledge?: (ruleId: string) => Promise<void>,
): UseAnchorAlarmResult {
  const audioContextRef = useRef<AudioContext | null>(null)
  const oscillatorsRef = useRef<OscillatorNode[]>([])
  const gainsRef = useRef<GainNode[]>([])
  const isPlayingRef = useRef(false)
  const wakeLockRef = useRef<WakeLockSentinel | null>(null)
  const [silenceError, setSilenceError] = useState<string | null>(null)

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

  // A same-device-only override that resumes the klaxon for an alarm the
  // server has already acknowledged (see `unsilence` below). Reset the
  // moment the underlying alarm stops being a plain acknowledged drag — the
  // condition cleared (alarm null) or re-raised fresh (phase back to
  // 'active') — so a stale override from a previous drag can never make the
  // next one start pre-unsilenced.
  const [forcedAudible, setForcedAudible] = useState(false)
  const isAcknowledged = alarm?.phase === 'acknowledged'
  useEffect(() => {
    if (!isAcknowledged) setForcedAudible(false)
  }, [isAcknowledged])

  const isActive = alarm !== null
  const isSilenced = isAcknowledged && !forcedAudible
  const isAlarming = isActive && (!isAcknowledged || forcedAudible)
  const ruleId = alarm?.rule_id ?? null

  /**
   * Acknowledges server-side so the alarm is silenced everywhere, and stops the
   * local klaxon immediately rather than waiting for the next stream event.
   *
   * If this device had locally resumed an already-acknowledged alarm via
   * `unsilence`, there is nothing new to tell the server — it already has
   * this acknowledged — so this only clears the local override and stops
   * the sound here.
   */
  const silence = useCallback(() => {
    stopKlaxon()
    setSilenceError(null)
    if (forcedAudible) {
      setForcedAudible(false)
      return
    }
    if (!ruleId || !acknowledge) return
    void acknowledge(ruleId).catch((err: unknown) => {
      setSilenceError(err instanceof Error ? err.message : String(err))
    })
  }, [ruleId, acknowledge, stopKlaxon, forcedAudible])

  const unsilence = useCallback(() => {
    if (isAcknowledged) setForcedAudible(true)
  }, [isAcknowledged])

  // Driven by whether it should currently be sounding, not by a transition, so
  // there is no state in which the loop fails to be (re)created.
  useEffect(() => {
    if (!isAlarming) {
      stopKlaxon()
      releaseWakeLock()
      return
    }

    void requestWakeLock()
    void playKlaxon()

    // playKlaxon pre-schedules ~16s of audio, so re-arm just before it runs out.
    const interval = window.setInterval(() => {
      if (!isPlayingRef.current) {
        void playKlaxon()
      }
    }, 16000)

    return () => {
      window.clearInterval(interval)
      stopKlaxon()
    }
  }, [isAlarming, playKlaxon, stopKlaxon, requestWakeLock, releaseWakeLock])

  // Release the wake lock on unmount; the effect above only covers transitions
  // while mounted.
  useEffect(() => () => { releaseWakeLock() }, [releaseWakeLock])

  void silenceError

  return { isAlarming, isSilenced, isActive, silence, unsilence }
}
