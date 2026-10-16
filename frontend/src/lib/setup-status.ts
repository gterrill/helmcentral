/**
 * First-run setup status (GET /api/setup/status). The server works out the
 * steps from live state; the Settings checklist and the dashboard line only
 * draw them. Mirrors backend/setup_status.go field-for-field.
 */

export type SetupStepState = 'done' | 'todo' | 'blocked'

export interface SetupStep {
  id: string
  title: string
  state: SetupStepState
  detail: string
  /** Where the step lands: a Settings section id and the DOM id of the control inside it. */
  target: { section: string; control: string }
}

export interface SetupStatus {
  steps: SetupStep[]
  complete: boolean
}

export const emptySetupStatus: SetupStatus = { steps: [], complete: false }

/** The first step still to do, or null when nothing is waiting. */
export function nextSetupStep(status: SetupStatus): SetupStep | null {
  return status.steps.find((s) => s.state === 'todo') ?? null
}

export function doneSetupCount(status: SetupStatus): number {
  return status.steps.filter((s) => s.state === 'done').length
}

/** A concrete verb for each step's button. */
export const SETUP_ACTION_LABELS: Record<string, string> = {
  connection: 'Check the connection',
  engines: 'Assign engines',
  house_bank: 'Choose the house bank',
  power: 'Choose power sources',
}

// A control to focus once Settings has drawn it. The dashboard line and the
// tiles ask for it, then navigate. The SignalK section acts on it when it
// mounts, and when a request arrives while it is already mounted. A request
// is good for five seconds: an old one must not steal focus later.
const REQUEST_TTL_MS = 5000
let pending: { control: string; at: number } | null = null
const listeners = new Set<() => void>()

export function requestSetupFocus(control: string): void {
  pending = { control, at: Date.now() }
  listeners.forEach((l) => l())
}

/** Calls cb whenever a focus request is made. Returns the unsubscribe. */
export function subscribeSetupFocus(cb: () => void): () => void {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

/** The live request, or null; an expired one is cleared. Does not consume it: a mount effect can run twice (StrictMode). */
export function peekPendingSetupFocus(): string | null {
  if (pending === null) return null
  if (Date.now() - pending.at > REQUEST_TTL_MS) {
    pending = null
    return null
  }
  return pending.control
}

export function clearPendingSetupFocus(): void {
  pending = null
}

export function takePendingSetupFocus(): string | null {
  const control = peekPendingSetupFocus()
  pending = null
  return control
}

/** Scrolls to and focuses the control with this DOM id. False when it is not drawn (yet). */
export function focusSetupControl(control: string): boolean {
  const el = document.getElementById(control)
  if (!el) return false
  el.scrollIntoView?.({ block: 'center' })
  el.focus({ preventScroll: true })
  return true
}
