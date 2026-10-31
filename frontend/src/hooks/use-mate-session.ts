import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { apiRequest } from '@/lib/api-query'
import { MATE_BUSY_MESSAGE, useAssistantChat, type AssistantScreenContext } from '@/hooks/use-assistant-chat'
import {
  useAssistantConversations,
  type AssistantMessage,
  type AssistantMessageAttachment,
} from '@/hooks/use-assistant-conversations'
import { errorMessage } from '@/lib/errors'

/**
 * One Mate surface's conversation (ADR 0183): which conversation is open,
 * its messages, asking Mate a question and reading the answer back
 * (ADR 0093, streamed per ADR 0105), rejoining an answer still being
 * written, and stopping one.
 *
 * This is the only Mate hook a component uses. The panel (AssistantDrawer)
 * and the sheet (MateSheet) each make their own, so each keeps its own open
 * conversation and messages (ADR 0173 keeps messages out of the query
 * store). The two hooks underneath, `useAssistantConversations` and
 * `useAssistantChat`, are this module's internals.
 */

export interface MateSessionOptions {
  /** Opens this conversation on mount, and again whenever it changes to a
   * new id ("Open in Mate", ADR 0094). Back to null starts a blank chat. */
  initialId?: string | null
  /** The screen the operator is on (ADR 0093, ADR 0167). Sent with every
   * question. A getter is called at send time; a value is read from the
   * latest render. */
  screen?: AssistantScreenContext | (() => AssistantScreenContext | undefined)
}

export interface MateSendOptions {
  /** Documents staged in the composer (ADR 0106). Their chips go on the
   * question bubble; their ids go to the server. */
  attachments?: AssistantMessageAttachment[]
  /** The question came in by voice: Mate writes a spoken summary for it. */
  spoken?: boolean
  /** Ask in a fresh conversation, even while another answer streams. */
  newConversation?: boolean
}

export interface MateSendResult {
  /** The answer arrived, whether or not its conversation is still open. */
  answered: boolean
  /** The answer, when it arrived while its conversation was still open. */
  message: AssistantMessage | null
  /** Set when the question was never sent. */
  refused?: 'busy'
}

/** The operator's verdict on one answer, as the server holds it. */
export interface MateAnswerRating {
  verdict: 'up' | 'down'
  note: string
}

export function useMateSession(options: MateSessionOptions = {}) {
  const conversations = useAssistantConversations({ initialId: options.initialId })
  const chat = useAssistantChat()
  const {
    activeId,
    create,
    appendLocal,
    refresh,
    reloadMessages,
    markRefused,
    select,
    startNew,
    remove,
    reload,
    updateProposal,
    updatePassageDecision,
    updateFormDraft,
    updateFeedback,
    holdFeedback,
    setSummaryNoteId,
  } = conversations
  const { send: chatSend, abort, attach, isStreamingConversation, answerDelivered, clearError } = chat

  // Answers delivered live to this surface, by message id and the
  // conversation they belong to. Only these can be rated (an answer read back
  // from an earlier visit cannot), so the set is emptied of any other
  // conversation as soon as the open one changes.
  const [liveAnswers, setLiveAnswers] = useState<Record<string, string>>({})
  const markLive = useCallback((message: AssistantMessage) => {
    if (message.role !== 'assistant') return
    setLiveAnswers((previous) => ({ ...previous, [message.id]: message.conversationId }))
  }, [])
  useEffect(() => {
    setLiveAnswers((previous) => {
      const kept = Object.entries(previous).filter(([, conversationId]) => conversationId === activeId)
      return kept.length === Object.keys(previous).length ? previous : Object.fromEntries(kept)
    })
  }, [activeId])
  const liveAnswerIds = useMemo<ReadonlySet<string>>(() => new Set(Object.keys(liveAnswers)), [liveAnswers])

  // Saves the verdict on an answer (a note rides with it), or clears it when
  // `verdict` is null. Resolves null on success, or the server's words when it
  // refused; the verdict shown stays the one the server holds. The rating lives
  // on the message, so reading the thread afresh replaces it with the server's.
  const rateAnswer = useCallback(async (messageId: string, verdict: 'up' | 'down' | null, note = ''): Promise<string | null> => {
    const path = `/api/assistant/messages/${encodeURIComponent(messageId)}/feedback`
    // Held so a re-read of the thread that began before this request committed
    // cannot put the older rating back.
    const settle = holdFeedback(messageId, verdict === null ? undefined : { verdict, note })
    try {
      if (verdict === null) {
        await apiRequest<void>(path, { method: 'DELETE' })
        updateFeedback(messageId, undefined)
      } else {
        await apiRequest<unknown>(path, { method: 'PUT', json: { verdict, note } })
        updateFeedback(messageId, { verdict, note })
      }
      settle(true)
      return null
    } catch (err) {
      settle(false)
      return errorMessage(err)
    }
  }, [updateFeedback, holdFeedback])

  // Verdicts by message id, as the thread holds them.
  const ratings = useMemo(() => {
    const byId: Record<string, MateAnswerRating> = {}
    for (const message of conversations.messages) {
      if (message.feedback) byId[message.id] = message.feedback
    }
    return byId
  }, [conversations.messages])

  const screenRef = useRef(options.screen)
  screenRef.current = options.screen

  // Which conversation this surface shows, ahead of the render that carries
  // it: a first question creates its conversation and streams into it
  // before React has re-rendered with the new id.
  const activeIdRef = useRef(activeId)
  activeIdRef.current = activeId

  const sendingRef = useRef(chat.sending)
  sendingRef.current = chat.sending
  // Questions this session has in flight, counted from the moment send()
  // is called until the answer's stream ends, so two sends in the same tick
  // cannot both go. The quiet re-read after an answer does not count.
  const inFlightRef = useRef(0)
  // The same, as state, so what shows can follow it.
  const [questionPending, setQuestionPending] = useState(false)
  // A conversation this session has just created and is about to send
  // into: the rejoin below must not look for a running answer there.
  const sendingIntoRef = useRef<string | null>(null)

  // "Stopped." after the operator cut an answer off, in the conversation it
  // happened in; cleared by the next question. Not an error: chat.error
  // stays null through a stop.
  const [stoppedIn, setStoppedIn] = useState<string | null>(null)
  // A question this session refused itself, worded like the server's 409,
  // and the conversation it was asked in. It shows only while that answer
  // is still being written.
  const [refusedIn, setRefusedIn] = useState<string | null>(null)

  const send = useCallback(async (text: string, sendOptions: MateSendOptions = {}): Promise<MateSendResult> => {
    const attachments = sendOptions.attachments ?? []
    // One question at a time per session (ADR 0183). A question for a fresh
    // conversation is the exception: the answer still streaming carries on
    // server side and the "Mate answered" toast covers it.
    if (!sendOptions.newConversation && (inFlightRef.current > 0 || sendingRef.current)) {
      setRefusedIn(activeIdRef.current)
      return { answered: false, message: null, refused: 'busy' }
    }
    inFlightRef.current += 1
    setQuestionPending(true)
    let released = false
    const release = () => {
      if (released) return
      released = true
      inFlightRef.current -= 1
      sendingIntoRef.current = null
      if (inFlightRef.current === 0) setQuestionPending(false)
    }
    try {
      setStoppedIn(null)
      setRefusedIn(null)

      let conversationId = sendOptions.newConversation ? null : activeIdRef.current
      if (conversationId === null) {
        conversationId = await create((id) => {
          sendingIntoRef.current = id
          activeIdRef.current = id
        })
        if (conversationId === null) return { answered: false, message: null }
      }

      const localId = `local-${Date.now()}`
      appendLocal({
        id: localId,
        clientKey: localId,
        conversationId,
        seq: -1,
        role: 'user',
        content: text,
        createdAt: new Date().toISOString(),
        attachments: attachments.length > 0 ? attachments : undefined,
      })

      // The reply is appended as the stream's final frame lands, in the same
      // render that drops the streamed draft, so the reply never leaves the
      // screen. It counts as answered from then on: Stop tapped before the
      // stream closes makes chat.send resolve null, but the answer is there.
      const askedIn = conversationId
      let answered = false
      let refused = false
      let shown: AssistantMessage | null = null
      const ids = attachments.map((a) => a.documentId)
      const screen = typeof screenRef.current === 'function' ? screenRef.current() : screenRef.current
      await chatSend(askedIn, text, {
        ...(sendOptions.spoken ? { spoken: true } : {}),
        ...(screen !== undefined ? { screen } : {}),
        ...(ids.length > 0 ? { attachments: ids } : {}),
        onRefused: () => {
          refused = true
        },
        onMessage: (message) => {
          answered = true
          if (activeIdRef.current !== askedIn) return
          shown = message
          markLive(message)
          appendLocal(message)
        },
      })
      release()

      // The thread now holds the optimistic question and the streamed
      // answer. Re-read it quietly so the question becomes the server's
      // copy. Not awaited: the session is free for the next question, and
      // the read is dropped if anything changed meanwhile.
      if (shown !== null) void Promise.all([refresh(), reloadMessages(askedIn)])
      // A refused question (not ready, busy) leaves no run and was never
      // stored: its bubble stays as typed and so does its notice.
      else if (refused) {
        markRefused(localId)
      }
      // A question that got no answer may have left a run behind (it ran out
      // of time, or was stopped): read the thread so the run shows in it, in
      // place of the live notice.
      else if (activeIdRef.current === askedIn) {
        void (cancelRef.current ?? Promise.resolve()).then(() => reloadMessages(askedIn)).then((runShown) => {
          cancelRef.current = null
          // The notice belongs to the conversation it was asked in.
          if (!runShown || activeIdRef.current !== askedIn) return
          setStoppedIn(null)
          clearError()
        })
      }
      return { answered, message: shown }
    } finally {
      release()
    }
  }, [create, appendLocal, markLive, chatSend, refresh, reloadMessages, markRefused, clearError])

  const isAnswering = useCallback(() => {
    const id = activeIdRef.current
    return id !== null && isStreamingConversation(id)
  }, [isStreamingConversation])

  // Stop (ADR 0105) tells the server to give up on the run. Stop after the
  // final frame arrived leaves a finished answer, not a stopped one.
  // The cancel request in flight after Stop. The server answers it only once
  // the run has stored its trace, so the re-read of the thread waits for it.
  const cancelRef = useRef<Promise<void> | null>(null)
  const stop = useCallback(() => {
    cancelRef.current = abort()
    if (!answerDelivered()) setStoppedIn(activeIdRef.current)
  }, [abort, answerDelivered])

  // Rejoins whatever answer the server is writing for the open
  // conversation, then reloads the thread so rows the server added
  // meanwhile (a watch's report, ADR 0160) show as well.
  const rejoin = useCallback(async () => {
    const id = activeIdRef.current
    if (id === null || isStreamingConversation(id)) return
    await attach(id, undefined, (message) => {
      if (activeIdRef.current !== id) return
      markLive(message)
      appendLocal(message)
    })
    if (activeIdRef.current !== id) return
    await Promise.all([refresh(), reloadMessages(id)])
  }, [isStreamingConversation, attach, appendLocal, markLive, refresh, reloadMessages])

  // ADR 0105 ("the answer outlives the page"): whenever the open
  // conversation becomes a real id, on mount or on switching, rejoin
  // whatever answer the server is still writing for it. Skipped when this
  // conversation's own question is streaming, or about to (a first question
  // creates the id and sends straight away). A stream for another
  // conversation is let go: attach() closes it locally (its run carries on
  // server side) so this surface never shows, or stops, another
  // conversation's run.
  useEffect(() => {
    const id = activeId
    if (id === null || id === sendingIntoRef.current || isStreamingConversation(id)) return
    let cancelled = false
    void (async () => {
      // A second run of this effect (StrictMode) supersedes the first the
      // same way a second attach() does; `cancelled` keeps the first quiet.
      let delivered = false
      await attach(id, undefined, (message) => {
        if (cancelled || activeIdRef.current !== id) return
        delivered = true
        markLive(message)
        appendLocal(message)
      })
      if (!delivered) return
      await refresh()
    })()
    return () => {
      cancelled = true
    }
  }, [activeId, isStreamingConversation, attach, appendLocal, markLive, refresh])

  const busy = chat.sending || questionPending
  const error = chat.error ?? (busy && refusedIn === activeId ? MATE_BUSY_MESSAGE : null)
  const stopped = stoppedIn !== null && stoppedIn === activeId
  const status = useMemo(() => ({
    sending: chat.sending,
    statusText: chat.statusText,
    draft: chat.draft,
    /** A failed or refused question, in words the operator can act on. */
    error,
    stopped,
    /** The last answer delivered on this session's stream; the thread
     * scrolls to its start. */
    deliveredMessageId: chat.deliveredMessageId,
    /** Row key of the streamed draft; the delivered reply carries it too. */
    draftKey: chat.draftKey,
  }), [chat.sending, chat.statusText, chat.draft, error, stopped, chat.deliveredMessageId, chat.draftKey])

  return useMemo(() => ({
    conversations: conversations.conversations,
    activeId,
    messages: conversations.messages,
    /** The thread in order: messages, and each run that gave no answer after its
     * question (ADR 0240). */
    timeline: conversations.timeline,
    summaryNoteId: conversations.summaryNoteId,
    setSummaryNoteId,
    /** The open conversation is still being read; its previous messages are
     * still showing. */
    conversationLoading: conversations.conversationLoading,
    /** The conversation list is loading. */
    loading: conversations.loading,
    /** A failure to load or change the conversations, in plain words. */
    errorMessage: conversations.errorMessage,
    status,
    send,
    stop,
    select,
    startNew,
    remove,
    reload,
    rejoin,
    isAnswering,
    reloadMessages,
    updateProposal,
    updatePassageDecision,
    updateFormDraft,
    /** Answers delivered live in the open conversation: the only ones the
     * operator can rate. */
    liveAnswerIds,
    /** Verdicts given this visit, by message id. */
    ratings,
    rateAnswer,
  }), [
    conversations.conversations,
    reloadMessages,
    activeId,
    conversations.messages,
    conversations.timeline,
    conversations.summaryNoteId,
    setSummaryNoteId,
    conversations.conversationLoading,
    conversations.loading,
    conversations.errorMessage,
    status,
    send,
    stop,
    select,
    startNew,
    remove,
    reload,
    rejoin,
    isAnswering,
    updateProposal,
    updatePassageDecision,
    updateFormDraft,
    liveAnswerIds,
    ratings,
    rateAnswer,
  ])
}

export type MateSession = ReturnType<typeof useMateSession>
