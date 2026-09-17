/**
 * Module-level record of Mate conversations this browser tab has asked a
 * question in and might have wandered away from before the answer landed
 * (mate-answer-toast plan, on top of ADR 0105's "the answer outlives the
 * page"). `use-assistant-chat.ts`'s `send()` registers a conversation here
 * the moment it posts a question; `use-mate-answer-watcher.ts` (mounted once
 * in App, outside the kiosk path) reads the list and opens `GET .../run` for
 * whatever entry isn't the conversation currently on screen, so a reply that
 * finishes while the operator is looking at another panel still gets a
 * toast.
 *
 * A plain module-level singleton, not React context or state: `send()` has
 * no reason to know whether anything is watching, and threading a setter
 * down through AssistantDrawer/MateSheet/useAssistantChat just to reach
 * App would be prop drilling for a concern those components don't otherwise
 * have. Same module-singleton-plus-useSyncExternalStore shape as
 * hooks/use-vessel-identity.ts and hooks/use-gauge-values.ts.
 */

export interface MateWatchEntry {
  conversationId: string
  /** `Date.now()` when the question that started this run was sent - the
   * 204 path (the run already finished before the watcher attached) uses
   * this to tell a fresh answer from one the operator already saw. */
  startedAt: number
}

type Listener = () => void

let entries: MateWatchEntry[] = []
const listeners = new Set<Listener>()

function notify(): void {
  for (const listener of listeners) listener()
}

/**
 * Starts (or restarts) watching `conversationId`. Called from `send()`, so
 * this always means "a question was just posted here" - a conversation
 * already being watched has its `startedAt` moved forward rather than
 * gaining a second entry, since only the newest question's answer matters
 * for the "is this newer than what the operator already saw" check on the
 * 204 path.
 */
export function registerMateWatch(conversationId: string, startedAt: number = Date.now()): void {
  entries = [...entries.filter((entry) => entry.conversationId !== conversationId), { conversationId, startedAt }]
  notify()
}

/** Stops watching `conversationId` - the toast (or the deliberate absence of
 * one) has already been decided, or the operator navigated onto it mid-run. */
export function removeMateWatch(conversationId: string): void {
  if (!entries.some((entry) => entry.conversationId === conversationId)) return
  entries = entries.filter((entry) => entry.conversationId !== conversationId)
  notify()
}

export function subscribeMateWatch(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function getMateWatchSnapshot(): readonly MateWatchEntry[] {
  return entries
}
