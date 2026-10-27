import { useCallback, useEffect, useState } from 'react'

import { subscribeTelemetry } from '@/hooks/use-telemetry-stream'
import { apiRequest } from '@/lib/api-query'
import { createTelemetryStore, type SubscribeTelemetry, type TelemetryUpdate } from '@/lib/telemetry-store'

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

function sameAutopilot(a: AutopilotState, b: AutopilotState): boolean {
  return a.present === b.present && a.engaged === b.engaged && a.state === b.state && a.mode === b.mode
    && a.target === b.target && a.stale === b.stale
    && a.availableActions.length === b.availableActions.length
    && a.availableActions.every((action, i) => action === b.availableActions[i])
}

/*
 * One store for the `autopilot` event (ADR 0215; lib/telemetry-store.ts). The
 * state is null until the first event: before it nobody knows whether there is
 * a pilot, and the tile must not say there is none. toAutopilotState builds a
 * new object for every payload, so an equal one keeps the previous object and
 * notifies nobody.
 */
function applyAutopilot(previous: AutopilotState | null, raw: string): TelemetryUpdate<AutopilotState | null> | null {
  let payload: AutopilotStreamPayload
  try {
    payload = JSON.parse(raw) as AutopilotStreamPayload
  } catch (err) {
    console.error('Failed to parse autopilot event:', err)
    return null
  }
  const next = toAutopilotState(payload)
  if (previous !== null && sameAutopilot(previous, next)) return null
  return { state: next }
}

// Read at start, not at import: a module that only imports this one's
// helpers or types never touches the stream.
const listenToStream: SubscribeTelemetry = (event, onData) => subscribeTelemetry(event, onData)

const store = createTelemetryStore<AutopilotState | null>(listenToStream, 'autopilot', null, applyAutopilot)

/** Starts the autopilot store on mount without re-rendering the caller. AppShell calls this once. */
export const useRetainAutopilot = store.useRetain

const selectState = (state: AutopilotState | null) => state
const selectPresent = (state: AutopilotState | null) => state?.present === true

/** What the pilot last reported, or null until the first autopilot event. */
export function useAutopilotState(): AutopilotState | null {
  return store.useSelector(selectState)
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
  const state = useAutopilotState()
  const [pending, setPending] = useState<Set<string>>(new Set())
  const [error, setError] = useState<string | null>(null)
  const [availableModes, setAvailableModes] = useState<string[]>([])
  const [capabilityError, setCapabilityError] = useState<string | null>(null)

  // One capability probe per pilot, not a poll: the supported mode list is
  // fixed for a given autopilot, so it is fetched once on mount and again
  // only if a pilot appears after the fact.
  //
  // A failed probe leaves the list empty and records why, rather than
  // substituting a plausible default set — offering modes the pilot may not
  // support would be exactly the kind of guess the project forbids, and the
  // tile disables the selector and shows this message instead.
  const present = store.useSelector(selectPresent)
  useEffect(() => {
    let cancelled = false

    void (async () => {
      try {
        const body = await apiRequest<AutopilotCapabilityBody>('/api/autopilot')
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
      await apiRequest(path, { method, json: body })
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
