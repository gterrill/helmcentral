import { useCallback, useEffect, useState } from 'react'

import { useTelemetryEvent } from '@/hooks/use-telemetry-stream'

export type AutopilotSide = 'port' | 'starboard'

/**
 * Live autopilot state, mirroring the backend's "autopilot" SSE payload
 * (backend/autopilot.go's buildAutopilotPayload, ADR 0041).
 *
 * `stale` reflects steering.autopilot.* having gone quiet on the delta
 * stream while a pilot is still `present` — the tile greys and says so
 * rather than trusting a frozen `engaged`.
 */
export interface AutopilotState {
  present: boolean
  engaged: boolean
  state: string | null
  mode: string | null
  target: number | null
  availableActions: string[]
  stale: boolean
}

const ABSENT_AUTOPILOT_STATE: AutopilotState = {
  present: false,
  engaged: false,
  state: null,
  mode: null,
  target: null,
  availableActions: [],
  stale: false,
}

type AutopilotStreamPayload = {
  present: boolean
  engaged?: boolean
  state?: string
  mode?: string
  target?: number
  available_actions?: string[]
  stale?: boolean
}

function toAutopilotState(payload: AutopilotStreamPayload): AutopilotState {
  if (!payload.present) return ABSENT_AUTOPILOT_STATE
  return {
    present: true,
    engaged: payload.engaged ?? false,
    state: payload.state ?? null,
    mode: payload.mode ?? null,
    target: payload.target ?? null,
    availableActions: payload.available_actions ?? [],
    stale: payload.stale ?? false,
  }
}

interface AutopilotErrorBody {
  error?: string
}

/**
 * SignalK's v2 autopilot status, as relayed verbatim by GET /api/autopilot.
 * Only `options.modes` is read here: which modes a pilot supports is static
 * capability data that the delta stream does not carry, so it cannot come
 * from the SSE payload like everything else does.
 */
interface AutopilotCapabilityBody {
  present?: boolean
  options?: { modes?: string[] }
}

/**
 * Live autopilot state and controls (ADR 0041).
 *
 * State comes exclusively from the "autopilot" SSE event — the same
 * no-polling pattern as use-gauge-values.ts — never from what a command
 * requested. Calling engage()/disengage()/tack()/gybe()/adjustHeading()
 * never mutates `state` directly: the tile only ever shows what the pilot
 * itself last reported on the stream, so a failed command can't leave the
 * tile lying about whether the boat is under autopilot.
 *
 * `pending` follows use-czone-switches.ts's precedent (a set keyed by
 * action, so a tapped control disables itself until the server answers) —
 * but without that hook's optimistic local-state update, which the safety
 * requirements here explicitly rule out.
 */
export function useAutopilot() {
  const [state, setState] = useState<AutopilotState>(ABSENT_AUTOPILOT_STATE)
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)
  const [availableModes, setAvailableModes] = useState<string[]>([])
  const [capabilityError, setCapabilityError] = useState<string | null>(null)

  useTelemetryEvent<AutopilotStreamPayload>('autopilot', (payload) => {
    setState(toAutopilotState(payload))
  })

  // One capability probe per pilot, not a poll: the supported mode list is
  // fixed for a given autopilot, so it is fetched once on mount and again
  // only if a pilot appears after the fact.
  //
  // A failed probe leaves the list empty and records why, rather than
  // substituting a plausible default set — offering modes the pilot may not
  // support would be exactly the kind of guess the project forbids, and the
  // tile disables the selector and shows this message instead.
  const present = state.present
  useEffect(() => {
    let cancelled = false

    void (async () => {
      try {
        const response = await fetch('/api/autopilot')
        if (!response.ok) {
          const body = (await response.json().catch(() => null)) as AutopilotErrorBody | null
          throw new Error(body?.error || `Could not read autopilot capabilities (${response.status})`)
        }
        const body = (await response.json()) as AutopilotCapabilityBody
        if (cancelled) return
        setAvailableModes(body.options?.modes ?? [])
        setCapabilityError(null)
      } catch (err) {
        if (cancelled) return
        setAvailableModes([])
        setCapabilityError(err instanceof Error ? err.message : 'Could not read autopilot capabilities')
      }
    })()

    return () => { cancelled = true }
  }, [present])

  const send = useCallback(async (action: string, path: string, method: string, body?: unknown) => {
    setPending((prev) => new Set(prev).add(action))
    setError(null)
    try {
      const response = await fetch(path, {
        method,
        ...(body !== undefined
          ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }
          : {}),
      })
      if (!response.ok) {
        const errorBody = (await response.json().catch(() => null)) as AutopilotErrorBody | null
        throw new Error(errorBody?.error || `Autopilot command failed (${response.status})`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Autopilot command failed')
    } finally {
      setPending((prev) => {
        const next = new Set(prev)
        next.delete(action)
        return next
      })
    }
  }, [])

  const engage = useCallback(() => send('engage', '/api/autopilot/engage', 'POST'), [send])
  const disengage = useCallback(() => send('disengage', '/api/autopilot/disengage', 'POST'), [send])
  const tack = useCallback(
    (side: AutopilotSide) => send(`tack-${side}`, `/api/autopilot/tack/${side}`, 'POST'),
    [send],
  )
  const gybe = useCallback(
    (side: AutopilotSide) => send(`gybe-${side}`, `/api/autopilot/gybe/${side}`, 'POST'),
    [send],
  )
  const adjustHeading = useCallback(
    (degrees: number) => send(`adjust-${degrees}`, '/api/autopilot/target/adjust', 'PUT', { value: degrees }),
    [send],
  )
  // The backend's mode handler binds {"mode": ...} and adds SignalK's own
  // {"value": ...} envelope itself (backend/autopilot.go).
  const setMode = useCallback(
    (mode: string) => send(`mode-${mode}`, '/api/autopilot/mode', 'PUT', { mode }),
    [send],
  )
  const dodge = useCallback(
    (degrees: number) => send(`dodge-${degrees}`, '/api/autopilot/dodge', 'PUT', { value: degrees }),
    [send],
  )
  const clearDodge = useCallback(
    () => send('dodge-clear', '/api/autopilot/dodge', 'DELETE'),
    [send],
  )

  return {
    state,
    pending,
    error,
    availableModes,
    capabilityError,
    engage,
    disengage,
    tack,
    gybe,
    adjustHeading,
    setMode,
    dodge,
    clearDodge,
  }
}
