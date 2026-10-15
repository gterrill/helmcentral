import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { MATE_BUSY_MESSAGE, useAssistantChat, type AssistantScreenContext } from '@/hooks/use-assistant-chat'
import {
  useAssistantConversations,
  type AssistantMessage,
  type AssistantMessageAttachment,
} from '@/hooks/use-assistant-conversations'

/**
 * One Mate surface's conversation (ADR 0180): which conversation is open,
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

export function useMateSession(options: MateSessionOptions = {}) {
  const conversations = useAssistantConversations({ initialId: options.initialId })
  const chat = useAssistantChat()
  const {
    activeId,
    create,
    appendLocal,
    refresh,
    reloadMessages,
    select,
    startNew,
    remove,
    reload,
    updateProposal,
    updateFormDraft,
    setSummaryNoteId,
  } = conversations
  const { send: chatSend, abort, attach, isStreamingConversation, answerDelivered } = chat

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
    // One question at a time per session (ADR 0180). A question for a fresh
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
      let shown: AssistantMessage | null = null
      const ids = attachments.map((a) => a.documentId)
      const screen = typeof screenRef.current === 'function' ? screenRef.current() : screenRef.current
      await chatSend(askedIn, text, {
        ...(sendOptions.spoken ? { spoken: true } : {}),
        ...(screen !== undefined ? { screen } : {}),
        ...(ids.length > 0 ? { attachments: ids } : {}),
        onMessage: (message) => {
          answered = true
          if (activeIdRef.current !== askedIn) return
          shown = message
          appendLocal(message)
        },
      })
      release()

      // The thread now holds the optimistic question and the streamed
      // answer. Re-read it quietly so the question becomes the server's
      // copy. Not awaited: the session is free for the next question, and
      // the read is dropped if anything changed meanwhile.
      if (shown !== null) void Promise.all([refresh(), reloadMessages(askedIn)])
      return { answered, message: shown }
    } finally {
      release()
    }
  }, [create, appendLocal, chatSend, refresh, reloadMessages])

  const isAnswering = useCallback(() => {
    const id = activeIdRef.current
    return id !== null && isStreamingConversation(id)
  }, [isStreamingConversation])

  // Stop (ADR 0105) tells the server to give up on the run. Stop after the
  // final frame arrived leaves a finished answer, not a stopped one.
  const stop = useCallback(() => {
    void abort()
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
      appendLocal(message)
    })
    if (activeIdRef.current !== id) return
    await Promise.all([refresh(), reloadMessages(id)])
  }, [isStreamingConversation, attach, appendLocal, refresh, reloadMessages])

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
        appendLocal(message)
      })
      if (!delivered) return
      await refresh()
    })()
    return () => {
      cancelled = true
    }
  }, [activeId, isStreamingConversation, attach, appendLocal, refresh])

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
  }), [chat.sending, chat.statusText, chat.draft, error, stopped, chat.deliveredMessageId])

  return useMemo(() => ({
    conversations: conversations.conversations,
    activeId,
    messages: conversations.messages,
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
    updateProposal,
    updateFormDraft,
  }), [
    conversations.conversations,
    activeId,
    conversations.messages,
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
    updateFormDraft,
  ])
}

export type MateSession = ReturnType<typeof useMateSession>
