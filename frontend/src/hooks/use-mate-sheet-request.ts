import { useCallback, useMemo, useRef, useState } from 'react'

import type { AssistantScreenContext } from '@/hooks/use-assistant-chat'
import { withAlarmContext, withWatchRunContext } from '@/lib/mate-screen'

/**
 * What the Mate sheet was opened for, and how long that lasts (ADR 0257).
 *
 * The sheet is one overlay rendered once in the app shell, so it keeps its
 * thread across opens and closes (ADR 0093, ADR 0183). Each open carries a
 * request: a question or draft, flags for how to send it, and the alarm or
 * watch run it is about (ADR 0226, ADR 0227). This hook owns that request,
 * resets it in one place, and layers its context over the screen the
 * operator is on.
 */

// The kinds of context a request can carry, each with the function that layers
// it over the screen. Add a kind here and nothing else: the ask options, the
// stored context and the screen all follow from this one table.
const CONTEXT_LAYERS = {
  alarm: withAlarmContext,
  watchRun: withWatchRunContext,
}

type ContextKind = keyof typeof CONTEXT_LAYERS
const CONTEXT_KINDS = Object.keys(CONTEXT_LAYERS) as ContextKind[]

/** What the sheet's conversation is about, beyond the screen it opened over. */
type MateSheetContext = { [K in ContextKind]?: NonNullable<Parameters<(typeof CONTEXT_LAYERS)[K]>[1]> }

export type MateAskOptions = MateSheetContext & {
  /** Ask in a fresh conversation rather than the one already open. */
  newConversation?: boolean
  /** The question came in by voice: only then does Mate write an answer meant
   * for speaking, and only then is it read aloud. */
  spoken?: boolean
  /** The question is text for the composer, not one to send. */
  draft?: boolean
}

interface MateSheetRequest {
  /** Names this open. 0 is no request. */
  id: number
  question: string | undefined
  newConversation: boolean
  draft: boolean
  spoken: boolean
  context: MateSheetContext
}

const NO_CONTEXT: MateSheetContext = {}
const NO_REQUEST: MateSheetRequest = {
  id: 0,
  question: undefined,
  newConversation: false,
  draft: false,
  spoken: false,
  context: NO_CONTEXT,
}

function contextFrom(options: MateAskOptions | undefined): MateSheetContext {
  if (!options) return NO_CONTEXT
  const context: Record<string, unknown> = {}
  for (const kind of CONTEXT_KINDS) {
    if (options[kind]) context[kind] = options[kind]
  }
  return Object.keys(context).length === 0 ? NO_CONTEXT : (context as MateSheetContext)
}

function applyContext(screen: AssistantScreenContext | undefined, context: MateSheetContext) {
  let layered = screen
  for (const kind of CONTEXT_KINDS) {
    // Each layer takes its own kind's value; the table keeps the pairs honest.
    const layer = CONTEXT_LAYERS[kind] as (s: AssistantScreenContext | undefined, value: unknown) => AssistantScreenContext | undefined
    layered = layer(layered, context[kind] ?? null)
  }
  return layered
}

export function useMateSheetRequest(baseScreen: AssistantScreenContext | undefined) {
  // `hasOpened` is set in the same update as `open`, so the lazy sheet mounts
  // on the render that first shows it, and stays mounted after: its thread,
  // active conversation and speech-output state survive closes.
  const [state, setState] = useState({ open: false, hasOpened: false })
  const [request, setRequestState] = useState<MateSheetRequest>(NO_REQUEST)
  // The latest request, readable without a render, so a call that changes
  // nothing can return before setting state (React may still render once for
  // an updater that returns the same value).
  const requestRef = useRef(NO_REQUEST)
  const setRequest = useCallback((next: MateSheetRequest) => {
    requestRef.current = next
    setRequestState(next)
  }, [])
  const nextIdRef = useRef(0)

  // Every open replaces the whole request, so nothing from the last one
  // survives into it.
  const ask = useCallback((question?: string, options?: MateAskOptions) => {
    nextIdRef.current += 1
    setRequest({
      id: nextIdRef.current,
      question,
      newConversation: Boolean(options?.newConversation),
      draft: Boolean(options?.draft),
      spoken: Boolean(options?.spoken),
      context: contextFrom(options),
    })
    setState({ open: true, hasOpened: true })
  }, [setRequest])

  // The sheet reports a request's context done once its question is sent (or
  // the operator starts or picks another chat). A send that began under an
  // earlier request must not clear a later one's context: a draft for run B
  // would go out unlinked because run A's send finished after it opened.
  const clearContext = useCallback((requestId: number) => {
    const current = requestRef.current
    if (current.id !== requestId || current.context === NO_CONTEXT) return
    setRequest({ ...current, context: NO_CONTEXT })
  }, [setRequest])

  // Closing drops the question so reopening later with none never re-sends a
  // stale one, and drops the context so a draft left unsent does not tie the
  // next, unrelated chat to its alarm or run.
  const onOpenChange = useCallback((next: boolean) => {
    setState((current) => ({ open: next, hasOpened: current.hasOpened || next }))
    if (!next) setRequest(NO_REQUEST)
  }, [setRequest])

  const screen = useMemo(() => applyContext(baseScreen, request.context), [baseScreen, request.context])

  const sheetProps = {
    open: state.open,
    onOpenChange,
    requestId: request.id,
    initialQuestion: request.question,
    newConversation: request.newConversation,
    draft: request.draft,
    spoken: request.spoken,
    screen,
    onContextDone: clearContext,
  }

  return { ask, open: state.open, hasOpened: state.hasOpened, sheetProps }
}
