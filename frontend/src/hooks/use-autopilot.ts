import { useEffect, useSyncExternalStore } from 'react'

import { apiRequest } from '@/lib/api-query'
import { createTelemetryStore, type TelemetryUpdate } from '@/lib/telemetry-store'
import { onTelemetryTestReset } from '@/lib/test-reset'

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
 * One store for the `autopilot` event (ADR 0215; lib/telemetry-store.ts). Its
 * initial state is the absent pilot, which is a placeholder: before the first
 * event nobody knows whether there is a pilot, so useAutopilotState hands out
 * nothing until the store has heard, and the tile must not say there is none.
 * toAutopilotState builds a new object for every payload, so an equal one keeps
 * the previous object and notifies nobody.
 */
function applyAutopilot(previous: AutopilotState, data: unknown): TelemetryUpdate<AutopilotState> | null {
  const next = toAutopilotState(data as AutopilotStreamPayload)
  return sameAutopilot(previous, next) ? null : { state: next }
}

const store = createTelemetryStore<AutopilotState>('autopilot', ABSENT_AUTOPILOT_STATE, applyAutopilot)

const selectState = (state: AutopilotState) => state
const selectPresent = (state: AutopilotState) => state.present

/** What the pilot last reported, or null until the first autopilot event (the tile's "--"). */
export function useAutopilotState(): AutopilotState | null {
  return store.useHeardSelector(selectState)
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

// ── the controls ─────────────────────────────────────────────────────────────
//
// The pending commands, the last command error and the capability probe belong
// to the pilot, not to one tile, so they live at module level beside the state
// store (ADR 0215). A tile unmounted while Engage is in flight, by a page
// switch or a wall rotation, comes back with Engage still pending and
// disabled, and a dashboard tile and the wall's tile never disagree about it.

interface AutopilotControls {
  pending: ReadonlySet<string>
  error: string | null
  availableModes: string[]
  capabilityError: string | null
}

const NO_CONTROLS: AutopilotControls = { pending: new Set(), error: null, availableModes: [], capabilityError: null }
let controls = NO_CONTROLS
const controlListeners = new Set<() => void>()

function setControls(patch: Partial<AutopilotControls> | ((current: AutopilotControls) => Partial<AutopilotControls>)): void {
  controls = { ...controls, ...(typeof patch === 'function' ? patch(controls) : patch) }
  for (const listener of controlListeners) listener()
}

function subscribeControls(listener: () => void): () => void {
  controlListeners.add(listener)
  return () => { controlListeners.delete(listener) }
}

const getControls = () => controls

// One capability probe per pilot, not a poll: the supported mode list is fixed
// for a given autopilot, so it is fetched once, for the first tile to mount,
// and again only if a pilot appears (or goes) after the fact.
//
// A failed probe is retried every PROBE_RETRY_MS for as long as at least one
// tile is mounted (wall pages stay mounted, so a mount alone never recovers),
// and on the next mount if none is. The error stays up until a probe succeeds.
//
// A failed probe leaves the list empty and records why, rather than
// substituting a plausible default set: offering modes the pilot may not
// support would be exactly the kind of guess the project forbids, and the tile
// disables the selector and shows this message instead.
let probedFor: boolean | null = null
let probeId = 0
const PROBE_RETRY_MS = 30_000
let consumers = 0
let retryTimer: ReturnType<typeof setTimeout> | null = null
// What the tiles last asked about, so a retry never re-asks a stale answer.
let wantedPresent = false

function clearRetry(): void {
  if (retryTimer !== null) clearTimeout(retryTimer)
  retryTimer = null
}

function probeCapabilities(present: boolean): void {
  wantedPresent = present
  if (probedFor === present) return
  probedFor = present
  const id = (probeId += 1)
  void (async () => {
    try {
      const body = await apiRequest<AutopilotCapabilityBody>('/api/autopilot')
      if (id !== probeId) return
      clearRetry()
      setControls({ availableModes: body.options?.modes ?? [], capabilityError: null })
    } catch (err) {
      if (id !== probeId) return
      // Forget the probe so the next tile to mount asks again.
      probedFor = null
      clearRetry()
      if (consumers > 0) {
        retryTimer = setTimeout(() => {
          retryTimer = null
          if (consumers > 0) probeCapabilities(wantedPresent)
        }, PROBE_RETRY_MS)
      }
      setControls({ availableModes: [], capabilityError: err instanceof Error ? err.message : 'Could not read autopilot capabilities' })
    }
  })()
}

onTelemetryTestReset(() => {
  controls = NO_CONTROLS
  probedFor = null
  probeId += 1
  consumers = 0
  clearRetry()
})

async function send(action: string, path: string, method: string, body?: unknown): Promise<void> {
  setControls((current) => ({ pending: new Set(current.pending).add(action), error: null }))
  try {
    await apiRequest(path, { method, json: body })
  } catch (err) {
    setControls({ error: err instanceof Error ? err.message : 'Autopilot command failed' })
  } finally {
    setControls((current) => {
      const pending = new Set(current.pending)
      pending.delete(action)
      return { pending }
    })
  }
}

const engage = () => send('engage', '/api/autopilot/engage', 'POST')
const disengage = () => send('disengage', '/api/autopilot/disengage', 'POST')
const tack = (side: AutopilotSide) => send(`tack-${side}`, `/api/autopilot/tack/${side}`, 'POST')
const gybe = (side: AutopilotSide) => send(`gybe-${side}`, `/api/autopilot/gybe/${side}`, 'POST')
const adjustHeading = (degrees: number) => send(`adjust-${degrees}`, '/api/autopilot/target/adjust', 'PUT', { value: degrees })
// The backend's mode handler binds {"mode": ...} and adds SignalK's own
// {"value": ...} envelope itself (backend/autopilot.go).
const setMode = (mode: string) => send(`mode-${mode}`, '/api/autopilot/mode', 'PUT', { mode })
const dodge = (degrees: number) => send(`dodge-${degrees}`, '/api/autopilot/dodge', 'PUT', { value: degrees })
const clearDodge = () => send('dodge-clear', '/api/autopilot/dodge', 'DELETE')

/**
 * Live autopilot state and controls (ADR 0041).
 *
 * State comes exclusively from the "autopilot" SSE event, never from what a
 * command requested. Calling engage()/disengage()/tack()/gybe()/adjustHeading()
 * never mutates `state` directly: the tile only ever shows what the pilot
 * itself last reported on the stream, so a failed command can't leave the
 * tile lying about whether the boat is under autopilot.
 *
 * `pending` follows use-czone-switches.ts's precedent (a set keyed by action,
 * so a tapped control disables itself until the server answers), but without
 * that hook's optimistic local-state update, which the safety requirements
 * here explicitly rule out. It is shared by every tile, as is the error, and
 * the commands are stable module functions.
 */
export function useAutopilot() {
  const state = useAutopilotState()
  const { pending, error, availableModes, capabilityError } = useSyncExternalStore(subscribeControls, getControls, getControls)
  const present = store.useSelector(selectPresent)
  useEffect(() => {
    consumers += 1
    return () => {
      consumers = Math.max(0, consumers - 1)
      if (consumers === 0) clearRetry()
    }
  }, [])
  useEffect(() => { probeCapabilities(present) }, [present])

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
