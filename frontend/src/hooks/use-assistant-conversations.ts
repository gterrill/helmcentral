import { useCallback, useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { ApiError, apiRequest, fetchQuery, getQueryData, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'
import type { MessageApi } from '@/lib/mate-api'
import { onPassageDecisionSaved } from '@/lib/passage-decisions-api'
import { decodeMateCards, type MateCard, type PassageDecision } from '@/lib/mate-cards'

// ADR 0093: the wire shape (Conversation/Message) is snake_case, persisted
// as-is by the assistant's SQLite store. Mapped to camelCase here the same
// way use-tide-chart.ts / use-weather-forecast.ts do it, so nothing else in
// the frontend has to think about the wire format.

interface ConversationApi {
  id: string
  title: string
  created_at: string
  updated_at: string
  /** The note this conversation was summarised into, when it still exists. */
  summary_note_id?: string
  /** The routine (ADR 0227) this conversation belongs to. */
  routine_id?: string
}

export type AssistantProposalStatus = 'pending' | 'applied' | 'dismissed' | 'stale'
export type AssistantProposalAction = 'create' | 'update' | 'delete'

interface ProposalOpApi {
  type: string
  action: AssistantProposalAction
  id?: string
  label?: string
  description: string
  before?: Record<string, unknown>
  after?: Record<string, unknown>
}

interface ProposalOpResultApi {
  type: string
  action: AssistantProposalAction
  id: string
  label?: string
  href?: string
}

export interface ProposalApi {
  id: string
  message_id: string
  status: AssistantProposalStatus
  ops: ProposalOpApi[]
  stale_reason?: string
  resolved_at?: string
  /** What Apply did, one entry per operation: present once applied. */
  result?: { ops: ProposalOpResultApi[] }
}

export type AssistantFormDraftStatus = 'draft' | 'saved' | 'dismissed'

export interface FormDraftApi {
  id: string
  message_id?: string
  source_document_id: string
  title: string
  folder: string
  filename: string
  page_count: number
  status: AssistantFormDraftStatus
  document_id?: string
}

/** A form Mate filled in (ADR 0165). It is a draft until the operator saves
 * it to Documents; `status` is the stored one, so a reloaded thread shows a
 * saved or dismissed card as it was left. */
export interface AssistantFormDraft {
  id: string
  messageId: string
  sourceDocumentId: string
  title: string
  folder: string
  filename: string
  pageCount: number
  status: AssistantFormDraftStatus
  /** The document the operator saved it as. */
  documentId?: string
}

export function mapFormDraft(api: FormDraftApi): AssistantFormDraft {
  return {
    id: api.id,
    messageId: api.message_id ?? '',
    sourceDocumentId: api.source_document_id,
    title: api.title,
    folder: api.folder,
    filename: api.filename,
    pageCount: api.page_count,
    status: api.status,
    documentId: api.document_id || undefined,
  }
}

export interface AssistantConversation {
  id: string
  title: string
  createdAt: string
  updatedAt: string
  /** Set when a Mate routine (ADR 0227) owns this conversation. */
  routineId?: string
}

export interface AssistantMessageAttachment {
  documentId: string
  filename: string
}

/** One change on a proposal card (ADR 0158): an action on a record of some
 * type. `description` is the operator-words line the server wrote
 * ("Generator · Oil and filter: every 250 h ..."); the client shows it as
 * given and never rebuilds it. `before` and `after` hold the fields the change
 * names, as they were and as they will be (a delete has only `before`).
 * `href` is the page that shows the record, set once the proposal is applied. */
export interface AssistantProposalOp {
  type: string
  action: AssistantProposalAction
  id?: string
  label?: string
  description: string
  before?: Record<string, unknown>
  after?: Record<string, unknown>
  href?: string
}

/** A change Mate proposed (ADR 0146, generalised by ADR 0158). `status` is the stored
 * one, so a reloaded thread shows applied and dismissed cards as they were
 * left. Nothing in a proposal has been written until it is `applied`. */
export interface AssistantProposal {
  id: string
  messageId: string
  status: AssistantProposalStatus
  ops: AssistantProposalOp[]
  /** Why a `stale` proposal could not be applied, in the server's words. */
  staleReason?: string
  resolvedAt?: string
}

export interface AssistantMessage {
  id: string
  /** The id of the optimistic bubble this message replaced, carried onto the
   * server's copy so the thread keeps rendering it as the same item. */
  clientKey?: string
  conversationId: string
  seq: number
  role: 'user' | 'assistant' | 'watch' | 'routine'
  content: string
  model?: string
  promptTokens?: number
  /** Part of promptTokens the provider served from its prompt cache. */
  cachedTokens?: number
  completionTokens?: number
  costUsd?: number
  toolRounds?: number
  createdAt: string
  /** Documents (ADR 0106) attached to this message - only ever present on a
   * user message; the backend never sets it on an assistant reply. */
  attachments?: AssistantMessageAttachment[]
  /** Change cards (ADR 0146, ADR 0158) - only ever on an assistant reply. */
  proposals?: AssistantProposal[]
  /** Filled-in forms (ADR 0165) - only ever on an assistant reply. */
  formDrafts?: AssistantFormDraft[]
  /** Cards built from tool results (ADR 0191) - only ever on an assistant reply. */
  cards?: MateCard[]
  /** The operator's rating of this answer, when it has one. */
  feedback?: { verdict: 'up' | 'down'; note: string }
}

export function mapProposal(api: ProposalApi): AssistantProposal {
  return {
    id: api.id,
    messageId: api.message_id,
    status: api.status,
    ops: api.ops.map((o, i) => ({
      type: o.type,
      action: o.action,
      id: o.id,
      label: o.label,
      description: o.description,
      before: o.before,
      after: o.after,
      href: api.result?.ops[i]?.href,
    })),
    staleReason: api.stale_reason,
    resolvedAt: api.resolved_at,
  }
}

const LIST_PATH = '/api/assistant/conversations'
const NO_CONVERSATIONS: AssistantConversation[] = []

interface ConversationListApi {
  conversations?: ConversationApi[]
}

// The `HTTP <n>` string describeLoadError reads, for a failed request; a
// network failure keeps whatever its TypeError carries.
function wireMessage(err: unknown): string {
  if (err instanceof ApiError) return `HTTP ${err.status}`
  return err instanceof Error ? err.message : String(err)
}

function mapConversation(api: ConversationApi): AssistantConversation {
  return { id: api.id, title: api.title, createdAt: api.created_at, updatedAt: api.updated_at, routineId: api.routine_id }
}

export function mapMessage(api: MessageApi): AssistantMessage {
  return {
    id: api.id,
    conversationId: api.conversation_id,
    seq: api.seq,
    role: api.role,
    content: api.content,
    model: api.model,
    promptTokens: api.prompt_tokens,
    cachedTokens: api.cached_tokens,
    completionTokens: api.completion_tokens,
    costUsd: api.cost_usd,
    toolRounds: api.tool_rounds,
    createdAt: api.created_at,
    attachments: api.attachments?.map((a) => ({ documentId: a.document_id, filename: a.filename })),
    proposals: api.proposals?.map(mapProposal),
    formDrafts: api.form_drafts?.map(mapFormDraft),
    cards: api.cards === undefined ? undefined : decodeMateCards(api.cards),
    feedback: api.feedback,
  }
}

function sameMessage(a: AssistantMessage, b: AssistantMessage): boolean {
  return JSON.stringify({ ...a, clientKey: undefined }) === JSON.stringify({ ...b, clientKey: undefined })
}

// The server's copy of a thread, swapped in without disturbing what is on
// screen: a message that has not changed keeps its object, so its bubble does
// not re-render or re-parse, and the server's copy of an optimistic question
// carries that bubble's key, so it is not remounted.
function mergeReloaded(previous: AssistantMessage[], fresh: AssistantMessage[]): AssistantMessage[] {
  const byId = new Map(previous.map((message) => [message.id, message]))
  const optimistic = previous.filter((message) => message.id.startsWith('local-'))
  return fresh.map((message) => {
    const old = byId.get(message.id)
    if (old) {
      if (sameMessage(old, message)) return old
      return old.clientKey ? { ...message, clientKey: old.clientKey } : message
    }
    if (message.role === 'user') {
      const index = optimistic.findIndex((local) => local.content === message.content)
      if (index !== -1) {
        const [local] = optimistic.splice(index, 1)
        return { ...message, clientKey: local.clientKey ?? local.id }
      }
    }
    return message
  })
}

export interface UseAssistantConversationsOptions {
  /** Selects this conversation on mount, when it is present in the freshly
   * fetched list (ADR 0094: "Open in Mate" hands the panel a thread the
   * sheet already has active). Mate UI cycle ("Mate opens on an empty
   * chat"): absent, unset, or not found in the list now means a blank
   * chat, not a fallback to the newest conversation - a stale or deleted id
   * is never a reason to guess at a different, unrelated thread. */
  initialId?: string | null
}

// A failed load surfaces `error` as the raw thrown message (an `HTTP <n>`
// string from fetchConversations/select, or whatever a network failure's
// TypeError carries) so tests and callers that want the exact wire detail
// still have it. Nothing operator-facing should ever print that string
// directly, though - this turns it into a plain sentence instead. 502/503/504
// specifically call out a dropped link, since on this boat that is what they
// almost always mean; any other HTTP status still names the code, since that
// is a real server-side detail worth keeping rather than masking; anything
// else (a network failure - "Failed to fetch", or any other TypeError) never
// got as far as a status code at all, so it reads that way instead of
// guessing at one.
export function describeLoadError(err: string): string {
  const httpMatch = /^HTTP (\d+)$/.exec(err)
  if (httpMatch) {
    const code = httpMatch[1]
    if (code === '502' || code === '503' || code === '504') {
      return `Mate's conversations could not be loaded. The server answered ${code}. This is usually the boat's link dropping for a moment.`
    }
    return `Mate's conversations could not be loaded (HTTP ${code}).`
  }
  return "Mate's conversations could not be loaded. The server did not answer."
}

export function useAssistantConversations(options?: UseAssistantConversationsOptions) {
  const initialId = options?.initialId ?? null
  // Captured once, at mount, via a lazy initializer the same way
  // App.tsx's `initialLocation` is - only the very first render's value
  // matters for the initial-selection effect below; a later change to
  // `initialId` is handled by the separate re-select effect further down,
  // not by re-running the mount fetch.
  const [mountInitialId] = useState(() => initialId)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [messages, setMessages] = useState<AssistantMessage[]>([])
  // The active conversation's summary note (ADR 0162), null until the
  // operator has saved one. Read from the conversation when it is opened and
  // set by the summary dialog after a save.
  const [summaryNoteId, setSummaryNoteId] = useState<string | null>(null)
  // True from the moment select() starts until its answer (or failure) is
  // applied: the previous conversation's messages are still showing, so
  // anything acting on "the active conversation" waits.
  const [conversationLoading, setConversationLoading] = useState(false)
  // A failure of select, create or remove. The list's own failure comes from
  // the query and clears when a later read succeeds.
  const [actionError, setActionError] = useState<string | null>(null)
  const [reloading, setReloading] = useState(false)

  const list = useApiQuery({
    queryKey: [LIST_PATH],
    path: LIST_PATH,
    select: (raw: ConversationListApi) =>
      Array.isArray(raw.conversations) ? raw.conversations.map(mapConversation) : NO_CONVERSATIONS,
  })
  const { refetch: refresh } = list
  const error = reloading ? null : (actionError ?? (list.error ? wireMessage(list.error) : null))

  // Code-review finding: startNew() (and remove(), when it clears the
  // selection outright rather than moving to a next conversation) used to
  // just reset local state, with no way to tell a select() already in
  // flight that its own answer no longer matters. A late response then
  // filled a chat the operator had already left - clicking a conversation,
  // then immediately pressing "New conversation" before the GET resolved,
  // left the blank chat re-populated with the abandoned conversation's
  // messages once it finally did. selectionSeqRef is bumped by every
  // action that changes which conversation (if any) is "current" - select
  // itself, startNew, and remove's own selection changes - and each
  // select() call only applies its own response if its own sequence number
  // is still the latest one by the time it resolves; anything newer already
  // won. The messages are local state the thread appends to as it streams,
  // which is why this read is not a keyed query.
  const selectionSeqRef = useRef(0)

  const select = useCallback(async (id: string) => {
    const seq = (selectionSeqRef.current += 1)
    setActiveId(id)
    setSummaryNoteId(null)
    setConversationLoading(true)
    try {
      const data = await apiRequest<{ conversation?: ConversationApi; messages?: MessageApi[] }>(
        `${LIST_PATH}/${encodeURIComponent(id)}`,
      )
      if (selectionSeqRef.current !== seq) return
      setMessages(Array.isArray(data.messages) ? data.messages.map(mapMessage) : [])
      setSummaryNoteId(data.conversation?.summary_note_id ?? null)
      setActionError(null)
      setConversationLoading(false)
    } catch (err) {
      if (selectionSeqRef.current !== seq) return
      setActionError(wireMessage(err))
      setMessages([])
      setConversationLoading(false)
    }
  }, [])

  // Shared by the mount effect below and by `reload`: reads the list afresh,
  // then opens `targetId` only when it names a conversation that actually
  // exists in it. Mate UI cycle ("Mate opens on an empty chat"): this used
  // to fall back to the most recently updated thread when `targetId` was
  // absent or stale, so a plain open silently resumed whatever the server
  // considered newest instead of starting blank, and a link naming a
  // since-deleted conversation landed on an unrelated one with no
  // indication anything was wrong. Neither happens any more - no match
  // means no selection, the same "fresh empty chat" state a mount with no
  // conversations at all has always shown. The operator (or an explicit
  // `initialId`/`select()`/`create()`) is the only thing that ever opens a
  // conversation now.
  const openTarget = useCallback(async (targetId: string | null) => {
    // Joins the read the mount just started instead of adding a second one.
    await fetchQuery([LIST_PATH], LIST_PATH)
    if (targetId === null) return
    const fresh = getQueryData<ConversationListApi>([LIST_PATH])
    if (fresh?.conversations?.some((c) => c.id === targetId)) await select(targetId)
  }, [select])

  // A later refresh never changes the selection; only the operator (or
  // create/remove/the re-select effect below, or an explicit `reload`) does
  // that.
  useEffect(() => {
    void openTarget(mountInitialId)
  }, [openTarget, mountInitialId])

  // Repeats the initial load on demand: a failed load otherwise has no way
  // out short of remounting the whole hook. Clears the stale error and
  // shows `loading` while it runs, then re-selects `mountInitialId` only
  // when one was given and still exists - the same rule the mount effect
  // uses, since a caller asking to reload wants the same starting point it
  // would have gotten on a fresh mount. With no `mountInitialId` this only
  // ever repopulates `conversations` - it never selects anything on its own.
  const reload = useCallback(async (): Promise<void> => {
    setActionError(null)
    setReloading(true)
    try {
      await openTarget(mountInitialId)
    } finally {
      setReloading(false)
    }
  }, [openTarget, mountInitialId])

  // Mate UI cycle ("Mate opens on an empty chat"): the sheet/panel's own
  // "New conversation" button used to call `create()`, which POSTs and
  // persists a brand new conversation row immediately - before the operator
  // had typed a word. Closing the sheet right after left an empty,
  // permanent row cluttering the list forever (the "empty persisted draft"
  // this cycle removes). `startNew` is the local-only equivalent: it clears
  // the active selection and the thread exactly the way a fresh mount
  // already does, with no request at all. The conversation is only ever
  // actually created, by `create()`, when the Mate session sends the first
  // real message (use-mate-session.ts) - so pressing "New conversation" and never sending anything now
  // leaves the list exactly as it was.
  //
  // Defined here, ahead of the re-select effect below, so that effect can
  // call it directly.
  const startNew = useCallback(() => {
    // Invalidates any select() already in flight (selectionSeqRef's own doc
    // comment above) - this is the "New conversation" side of the race.
    selectionSeqRef.current += 1
    setActiveId(null)
    setMessages([])
    setSummaryNoteId(null)
    setConversationLoading(false)
    setActionError(null)
  }, [])

  // Re-selects when `initialId` changes to a new non-null value after mount
  // (ADR 0094): "Open in Mate" can send an already-showing panel a different
  // conversation than the one it has open, and re-selecting in place is what
  // lets the caller avoid remounting the panel just to pick it up. Guarded
  // against firing on mount itself - the effect above already resolved the
  // initial selection - by comparing against the previous value rather than
  // running unconditionally whenever `initialId` is non-null.
  //
  // Code-review finding ("clicking Mate ALWAYS shows a fresh empty chat"):
  // the same comparison also has to cover `initialId` going the OTHER way,
  // back to null - App.tsx's sidebar Mate click clears its remembered
  // conversation id rather than remounting the panel (AssistantDrawer stays
  // mounted whenever the operator merely re-clicks the panel already
  // showing), so without this branch a panel already showing some
  // conversation kept showing it forever no matter how many times "Mate"
  // was clicked again.
  const previousInitialIdRef = useRef(initialId)
  useEffect(() => {
    const previous = previousInitialIdRef.current
    previousInitialIdRef.current = initialId
    if (initialId !== null && initialId !== previous) {
      void select(initialId)
    } else if (initialId === null && previous !== null) {
      startNew()
    }
  }, [initialId, select, startNew])

  // Creates a conversation and opens it. A new conversation has no messages
  // and no summary note, so it opens straight from the POST's answer with
  // no read. `onCreated` runs before the new id becomes active, so a caller
  // can mark it (the Mate session is about to send into it and must not
  // look for a running answer there).
  const create = useCallback(async (onCreated?: (id: string) => void): Promise<string | null> => {
    try {
      const created = mapConversation(await apiRequest<ConversationApi>(LIST_PATH, { method: 'POST', json: {} }))
      invalidateQueries([LIST_PATH])
      selectionSeqRef.current += 1
      onCreated?.(created.id)
      setActiveId(created.id)
      setMessages([])
      setSummaryNoteId(null)
      setConversationLoading(false)
      setActionError(null)
      return created.id
    } catch (err) {
      setActionError(wireMessage(err))
      return null
    }
  }, [])

  const remove = useCallback(async (id: string) => {
    try {
      try {
        await apiRequest<void>(`${LIST_PATH}/${encodeURIComponent(id)}`, { method: 'DELETE' })
      } catch (err) {
        // A 404 here means the conversation is already gone server-side,
        // which is the outcome the caller wanted anyway - not a failure to
        // surface.
        if (err instanceof ApiError && err.status === 409) {
          // A routine's conversation (ADR 0227): the server says which routine
          // to delete first. Nothing is wrong with the list, so no list error.
          toast.error(err.message)
          return
        }
        if (!(err instanceof ApiError && err.status === 404)) throw err
      }

      // The next conversation to open comes from the list as it is after the
      // delete, so this read is awaited and a failure of it is reported.
      setQueryData([LIST_PATH], await apiRequest<ConversationListApi>(LIST_PATH))
      setActionError(null)

      if (activeId === id) {
        const next = getQueryData<ConversationListApi>([LIST_PATH])?.conversations?.[0] ?? null
        if (next) {
          await select(next.id)
        } else {
          // Same invalidation as startNew - nothing left to select, so any
          // select() still in flight for the conversation just deleted must
          // not be allowed to repopulate the now-empty chat either.
          selectionSeqRef.current += 1
          setActiveId(null)
          setMessages([])
          setSummaryNoteId(null)
          setConversationLoading(false)
        }
      }
    } catch (err) {
      setActionError(wireMessage(err))
    }
  }, [activeId, select])

  // Bumped by every local append, so a quiet reload that was already in
  // flight when a new message landed knows its answer is out of date.
  const localAppendsRef = useRef(0)
  const feedbackVersionRef = useRef(0)
  const reloadsInFlightRef = useRef(0)
  // Passage decisions saved while a reload may be in flight, by card id, so an
  // older server copy cannot replace them. Cleared once no reload is running.
  const decisionVersionRef = useRef(0)
  const pendingDecisionsRef = useRef(new Map<number, { decision: PassageDecision; version: number }>())
  const pendingFeedbackRef = useRef(new Map<string, { feedback: AssistantMessage['feedback']; version: number; settled: boolean }>())

  // Appends a message already known to the caller - the optimistic user
  // bubble sent before the server has responded, and the final assistant
  // message once it has. Never issues a request itself.
  const appendLocal = useCallback((message: AssistantMessage) => {
    localAppendsRef.current += 1
    setMessages((previous) => [...previous, message])
  }, [])

  // Re-reads the open conversation's messages without the loading state
  // select() shows, so the thread does not flash. Used once an answer has
  // landed, to swap the optimistic question bubble for the server's copy.
  // The answer is dropped when anything changed meanwhile: another
  // conversation opened (the selection sequence moved) or a message was
  // appended locally (the read may predate it).
  const reloadMessages = useCallback(async (id: string) => {
    const seq = selectionSeqRef.current
    const appends = localAppendsRef.current
    const feedbackVersion = feedbackVersionRef.current
    const decisionVersion = decisionVersionRef.current
    reloadsInFlightRef.current += 1
    try {
      const data = await apiRequest<{ conversation?: ConversationApi; messages?: MessageApi[] }>(
        `${LIST_PATH}/${encodeURIComponent(id)}`,
      )
      if (selectionSeqRef.current !== seq || localAppendsRef.current !== appends) return
      const fresh = Array.isArray(data.messages) ? data.messages.map(mapMessage) : []
      // A rating changed after this read began, or still being saved, is newer
      // than what the read saw: keep it rather than the server's older copy.
      const newer = fresh.map((message) => {
        const pending = pendingFeedbackRef.current.get(message.id)
        return pending && (!pending.settled || pending.version > feedbackVersion) ? { ...message, feedback: pending.feedback } : message
      })
      const withDecisions = newer.map((message) =>
        message.cards?.some((c) => c.kind === 'passage_window' && c.cardId !== undefined && (pendingDecisionsRef.current.get(c.cardId)?.version ?? 0) > decisionVersion)
          ? {
              ...message,
              cards: message.cards.map((c) => {
                const held = c.kind === 'passage_window' && c.cardId !== undefined ? pendingDecisionsRef.current.get(c.cardId) : undefined
                return held && held.version > decisionVersion ? { ...c, decision: held.decision } : c
              }),
            }
          : message,
      )
      setMessages((previous) => mergeReloaded(previous, withDecisions))
      setSummaryNoteId(data.conversation?.summary_note_id ?? null)
    } catch (err) {
      if (selectionSeqRef.current !== seq) return
      setActionError(wireMessage(err))
    } finally {
      reloadsInFlightRef.current -= 1
      if (reloadsInFlightRef.current === 0) {
        pendingDecisionsRef.current.clear()
        for (const [messageId, pending] of pendingFeedbackRef.current) {
          if (pending.settled) pendingFeedbackRef.current.delete(messageId)
        }
      }
    }
  }, [])

  // Replaces one proposal's card state with what the server just answered
  // (apply or dismiss), so the thread and a later reload agree.
  const updateProposal = useCallback((proposal: AssistantProposal) => {
    setMessages((previous) =>
      previous.map((message) =>
        message.proposals?.some((p) => p.id === proposal.id)
          ? { ...message, proposals: message.proposals.map((p) => (p.id === proposal.id ? proposal : p)) }
          : message,
      ),
    )
  }, [])

  // The same for a passage plan's decision: the card with this id takes the
  // view the server just answered, so a reload agrees.
  const updatePassageDecision = useCallback((cardId: number, decision: PassageDecision) => {
    if (reloadsInFlightRef.current > 0) {
      decisionVersionRef.current += 1
      pendingDecisionsRef.current.set(cardId, { decision, version: decisionVersionRef.current })
    }
    setMessages((previous) =>
      previous.map((message) =>
        message.cards?.some((c) => c.kind === 'passage_window' && c.cardId === cardId)
          ? {
              ...message,
              cards: message.cards.map((c) => (c.kind === 'passage_window' && c.cardId === cardId ? { ...c, decision } : c)),
            }
          : message,
      ),
    )
  }, [])

  // The server re-matches passages when it saves a decision, so other cards'
  // inferred choices can change too, and a change made on the Passages page
  // never reaches this thread. After any saved decision the open thread is read
  // again. The card just saved is held by pendingDecisionsRef while that read
  // is in flight.
  useEffect(() => {
    if (activeId === null) return undefined
    return onPassageDecisionSaved(() => { void reloadMessages(activeId) })
  }, [activeId, reloadMessages])

  // The same for a form draft's Save and Dismiss.
  const updateFormDraft = useCallback((draft: AssistantFormDraft) => {
    setMessages((previous) =>
      previous.map((message) =>
        message.formDrafts?.some((d) => d.id === draft.id)
          ? { ...message, formDrafts: message.formDrafts.map((d) => (d.id === draft.id ? draft : d)) }
          : message,
      ),
    )
  }, [])

  // An answer's rating, as just saved (or taken back, when `feedback` is
  // undefined). Kept on the message, so a fresh read of the thread replaces it.
  // A rating the operator has just asked for, held until a re-read of the
  // thread cannot overwrite it with an older copy: from the moment the request
  // is sent, and (once it has settled) for any read that began before then.
  // `settle(true)` after the request succeeded, `settle(false)` after it was
  // refused, which lets the server's copy stand.
  const holdFeedback = useCallback((messageId: string, feedback: AssistantMessage['feedback']) => {
    feedbackVersionRef.current += 1
    const entry = { feedback, version: feedbackVersionRef.current, settled: false }
    pendingFeedbackRef.current.set(messageId, entry)
    return (succeeded: boolean) => {
      if (pendingFeedbackRef.current.get(messageId) !== entry) return
      if (!succeeded || reloadsInFlightRef.current === 0) {
        pendingFeedbackRef.current.delete(messageId)
        return
      }
      feedbackVersionRef.current += 1
      entry.version = feedbackVersionRef.current
      entry.settled = true
    }
  }, [])

  const updateFeedback = useCallback((messageId: string, feedback: AssistantMessage['feedback']) => {
    setMessages((previous) =>
      previous.map((message) => (message.id === messageId ? { ...message, feedback } : message)),
    )
  }, [])

  const errorMessage = error !== null ? describeLoadError(error) : null

  return {
    conversations: list.data ?? NO_CONVERSATIONS,
    activeId,
    messages,
    summaryNoteId,
    setSummaryNoteId,
    conversationLoading,
    loading: list.isLoading || reloading,
    error,
    errorMessage,
    select,
    create,
    startNew,
    remove,
    appendLocal,
    reloadMessages,
    updateProposal,
    updatePassageDecision,
    updateFormDraft,
    updateFeedback,
    holdFeedback,
    refresh,
    reload,
  }
}
