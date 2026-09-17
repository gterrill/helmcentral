import { useEffect, useRef, useSyncExternalStore } from 'react'
import { toast } from 'sonner'

import { apiBaseUrl } from '@/config/api'
import { readServerSentEvents } from '@/lib/sse-reader'
import { getMateWatchSnapshot, removeMateWatch, subscribeMateWatch } from '@/lib/mate-watch-store'

interface ConversationApi {
  id: string
  title: string
  created_at: string
  updated_at: string
}

interface MessageApi {
  id: string
  conversation_id: string
  role: 'user' | 'assistant'
  created_at: string
}

function firstLine(text: string): string {
  const index = text.indexOf('\n')
  return index === -1 ? text : text.slice(0, index)
}

/**
 * The run had already finished by the time this watcher attached (the
 * ordinary `GET .../run` 204 case). The only way to tell "an answer landed
 * while the operator wasn't looking" from "nothing new happened" is to
 * compare the newest message's timestamp against `freshSinceMs` - the
 * question's send time for a conversation that's never been viewed since,
 * or the moment the operator stopped viewing it otherwise (see
 * `useMateAnswerWatcher`'s `everViewed` tracking below).
 */
async function checkAlreadyFinishedAnswer(
  conversationId: string,
  freshSinceMs: number,
  signal: AbortSignal,
  onOpen: (conversationId: string) => void,
): Promise<void> {
  const response = await fetch(
    `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}`,
    { signal },
  )
  if (signal.aborted) return
  if (!response.ok) {
    console.error(`use-mate-answer-watcher: GET the conversation for ${conversationId} answered HTTP ${response.status}`)
    return
  }

  const data = (await response.json()) as { conversation?: ConversationApi; messages?: MessageApi[] }
  if (signal.aborted) return

  const messages = data.messages ?? []
  const last = messages[messages.length - 1]
  if (!last || last.role !== 'assistant') return
  const createdAtMs = new Date(last.created_at).getTime()
  if (Number.isNaN(createdAtMs) || createdAtMs <= freshSinceMs) return

  toast('Mate answered', {
    description: data.conversation?.title ?? '',
    action: { label: 'Open', onClick: () => onOpen(conversationId) },
  })
}

/**
 * Opens `GET .../run` for one watched-and-not-viewed conversation and turns
 * whatever it says into a toast, or silence. Mirrors `use-assistant-chat.ts`'s
 * `attach()` (same 204/status/delta/message/error shape), but this only ever
 * cares about the terminal `message`/`error` frame - there is no thread on
 * screen for a `status`/`delta` frame to usefully update.
 */
async function watchOneConversation(
  conversationId: string,
  freshSinceMs: number,
  signal: AbortSignal,
  onOpen: (conversationId: string) => void,
): Promise<void> {
  try {
    const response = await fetch(
      `${apiBaseUrl}/api/assistant/conversations/${encodeURIComponent(conversationId)}/run`,
      { signal },
    )

    if (response.status === 204) {
      await checkAlreadyFinishedAnswer(conversationId, freshSinceMs, signal, onOpen)
      return
    }

    if (!response.ok || response.body === null) {
      console.error(`use-mate-answer-watcher: GET .../run for ${conversationId} answered HTTP ${response.status}`)
      return
    }

    // Two flat, independently-nullable locals rather than one compound
    // object: `use-assistant-chat.ts`'s `consumeStream` sets a single such
    // local (`resolved`) from inside this same kind of callback and only
    // ever returns it whole afterward - reading a *member* of a narrowed
    // `let` that a closure reassigned, the way an earlier version of this
    // function did, confused TS's control-flow analysis into typing the
    // narrowed value as `never`. Flat locals sidestep it.
    let resolvedConversationId: string | null = null
    let resolvedConversationTitle = ''
    let errorText: string | null = null

    await readServerSentEvents(response.body, (event) => {
      if (event.event === 'message') {
        const data = JSON.parse(event.data) as { message: MessageApi; conversation: ConversationApi }
        resolvedConversationId = data.conversation.id
        resolvedConversationTitle = data.conversation.title
      } else if (event.event === 'error') {
        const data = JSON.parse(event.data) as { error?: string }
        errorText = typeof data.error === 'string' && data.error !== '' ? data.error : 'assistant error'
      }
    }, signal)

    if (signal.aborted) return

    if (resolvedConversationId !== null) {
      const openId = resolvedConversationId
      toast('Mate answered', {
        description: resolvedConversationTitle,
        action: { label: 'Open', onClick: () => onOpen(openId) },
      })
    } else if (errorText !== null && errorText !== 'stopped') {
      // "stopped" is what a Stop button click produces (ADR 0105's
      // postAssistantRunCancelHandler) - operator-initiated, so it gets no
      // toast, the same as chat.abort() shows no error in the thread either.
      toast.error("Mate couldn't answer", {
        description: firstLine(errorText),
        action: { label: 'Open', onClick: () => onOpen(conversationId) },
      })
    }
    // Neither a message nor an error arrived (the stream just ended, e.g.
    // this watcher was torn down before the run produced anything): nothing
    // to tell the operator yet, and the conversation stays out of the watch
    // list either way (see the `finally` below) - a later question re-adds
    // it via send() if there's something new to watch for.
  } catch (err) {
    if (signal.aborted) return
    console.error(`use-mate-answer-watcher: watching conversation ${conversationId} failed`, err)
  }
}

/**
 * Mounted once in App.tsx, never on the kiosk path (mate-answer-toast plan,
 * on top of ADR 0105's "the answer outlives the page"): opens `GET .../run`
 * for every conversation `use-assistant-chat.ts`'s `send()` registered in
 * `lib/mate-watch-store.ts` that isn't the one currently on screen, and
 * turns whatever it finds into a toast with an "Open" action.
 *
 * `viewedConversationIds` is whichever conversation(s) the Mate page and/or
 * the Mate sheet currently have active - App.tsx works this out from
 * `activePanel`/`matePanelConversationId` and `mateSheetOpen`/
 * `mateSheetConversationId`. A watched conversation that's viewed never gets
 * a stream opened for it (the thread already shows whatever happens to it),
 * and one that becomes viewed while a stream is already open has that
 * stream closed with no toast - the operator navigated onto it themselves.
 *
 * `everViewed`/`freshSinceOverride` cover the case that isn't in either of
 * those two buckets: a conversation viewed continuously from the moment it
 * was registered, whose answer finishes while still on screen (so no stream
 * was ever opened for it), and is only left sometime after. Without
 * remembering that it was seen, leaving afterward would open a stream for
 * the first time, find the run already finished (204), and - since the
 * newest message really is newer than the original send time - fire a
 * stale toast for an answer already read on screen. Tracking the moment a
 * watched conversation stops being viewed and using that as the freshness
 * baseline instead fixes it: anything already shown before that moment
 * never counts as "missed".
 *
 * `enabled` (default true) is App.tsx's kiosk gate: the wall display never
 * reaches any Mate UI at all (see App.tsx's `isKiosk` early return, well
 * before `<MateSheet>`/the Mate panel are ever reachable), so its own watch
 * store can never actually hold anything in practice - `enabled` exists so
 * the hook still visibly does nothing on that path, rather than relying on
 * that indirect guarantee. Called unconditionally either way (rules of
 * hooks), the same way `useMateVoice`/`useKioskRotation` are elsewhere in
 * App.tsx - `false` closes whatever this instance had open rather than
 * orphaning it.
 */
export function useMateAnswerWatcher(
  viewedConversationIds: ReadonlySet<string>,
  onOpen: (conversationId: string) => void,
  enabled = true,
): void {
  const watched = useSyncExternalStore(subscribeMateWatch, getMateWatchSnapshot)
  const controllersRef = useRef(new Map<string, AbortController>())
  const everViewedRef = useRef(new Set<string>())
  const freshSinceOverrideRef = useRef(new Map<string, number>())
  const onOpenRef = useRef(onOpen)
  onOpenRef.current = onOpen

  useEffect(() => {
    const controllers = controllersRef.current
    const everViewed = everViewedRef.current
    const freshSinceOverride = freshSinceOverrideRef.current
    const effectiveWatched = enabled ? watched : []

    for (const entry of effectiveWatched) {
      const viewedNow = viewedConversationIds.has(entry.conversationId)
      if (viewedNow) {
        everViewed.add(entry.conversationId)
      } else if (everViewed.has(entry.conversationId)) {
        everViewed.delete(entry.conversationId)
        freshSinceOverride.set(entry.conversationId, Date.now())
      }
    }

    // Close (or never open) a stream for anything no longer watched, or
    // that just became the conversation on screen. The latter drops the
    // entry with no toast: the operator navigated onto it themselves, and
    // the thread is what shows it now.
    for (const [conversationId, controller] of controllers) {
      const stillWatched = effectiveWatched.some((entry) => entry.conversationId === conversationId)
      const viewedNow = viewedConversationIds.has(conversationId)
      if (!stillWatched || viewedNow) {
        controller.abort()
        controllers.delete(conversationId)
        if (stillWatched && viewedNow) removeMateWatch(conversationId)
      }
    }

    for (const entry of effectiveWatched) {
      const conversationId = entry.conversationId
      if (viewedConversationIds.has(conversationId)) continue
      if (controllers.has(conversationId)) continue

      const freshSinceMs = freshSinceOverride.get(conversationId) ?? entry.startedAt
      const controller = new AbortController()
      controllers.set(conversationId, controller)
      void watchOneConversation(conversationId, freshSinceMs, controller.signal, (id) => onOpenRef.current(id))
        .finally(() => {
          // Only the watch that still owns this conversation's entry in the
          // map cleans up - a StrictMode-doubled or otherwise superseded
          // attempt that lost the race must not remove a live watch's work
          // out from under it.
          if (controllers.get(conversationId) === controller) {
            controllers.delete(conversationId)
            freshSinceOverride.delete(conversationId)
            removeMateWatch(conversationId)
          }
        })
    }
  }, [watched, viewedConversationIds, enabled])

  // True unmount only - closes every stream this hook instance still owns.
  // Reusing the same ref-backed Map across a StrictMode mount/cleanup/mount
  // (same pattern as hooks/use-server-trails.ts) means the reconciliation
  // effect above simply reopens whatever this cleanup just closed, rather
  // than ever running two live streams for the same conversation at once.
  useEffect(() => {
    const controllers = controllersRef.current
    return () => {
      for (const controller of controllers.values()) controller.abort()
      controllers.clear()
    }
  }, [])
}
