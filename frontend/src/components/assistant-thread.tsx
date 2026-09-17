import { Loader2, Square } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type Ref } from 'react'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { Bubble, BubbleContent } from '@/components/ui/bubble'
import { Button } from '@/components/ui/button'
import { Marker, MarkerContent, MarkerIcon } from '@/components/ui/marker'
import { Message, MessageContent, MessageFooter } from '@/components/ui/message'
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerItem,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '@/components/ui/message-scroller'
import { Textarea } from '@/components/ui/textarea'
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage, useAssistantConversations } from '@/hooks/use-assistant-conversations'

const EXAMPLE_QUESTION =
  "We're at Hook Reef. Should we visit Tongue Bay or Blue Pearl Bay first over the next two days?"

// ADR 0093: every reply's footer names what it cost, in full - which model
// answered, how many tokens it used, and the price - so the running cost of
// asking questions is never a surprise. Any part the server didn't report
// (an optimistic local bubble has none of these yet) reads as `--` rather
// than a confident-looking zero.
//
// [impeccable critique 2026-09-12, PFD Learning #18] the price is the one
// decision-relevant figure here - the model slug and token count are
// backend mechanics, not something the operator reads at a glance to
// decide anything. formatMessageFooter leads with cost (plus a tool-round
// count when the reply made one), and formatMessageFooterTitle carries the
// model/tokens detail into the footer's `title` tooltip instead.
function formatMessageFooter(message: AssistantMessage): string {
  const cost = typeof message.costUsd === 'number' ? `$${message.costUsd.toFixed(3)}` : '--'
  const model = message.model && message.model !== '' ? message.model : '--'
  const parts = [cost, model]
  if (typeof message.toolRounds === 'number') {
    parts.push(`${message.toolRounds} tool round${message.toolRounds === 1 ? '' : 's'}`)
  }
  return parts.join(' · ')
}

function formatMessageFooterTitle(message: AssistantMessage): string {
  const model = message.model && message.model !== '' ? message.model : '--'
  const tokens =
    typeof message.promptTokens === 'number' && typeof message.completionTokens === 'number'
      ? (message.promptTokens + message.completionTokens).toLocaleString()
      : '--'
  return `${model} · ${tokens} tokens`
}

interface AssistantThreadProps {
  canWrite: boolean
  conversations: ReturnType<typeof useAssistantConversations>
  chat: ReturnType<typeof useAssistantChat>
  /** Focuses the composer as soon as it mounts - the Mate sheet (ADR 0093
   * voice phase) wants this when it opens with no question already in
   * flight; the panel never sets it, matching its pre-extraction behaviour. */
  autoFocus?: boolean
  /** Forwarded straight to the composer's textarea (ADR 0094): the sheet's
   * "New conversation" button focuses it directly after `create()` resolves,
   * which `autoFocus` alone can't do since that only ever fires on mount. */
  composerRef?: Ref<HTMLTextAreaElement>
}

/**
 * The message list, footer, status/error rows and composer for one Mate
 * conversation (ADR 0093, streamed per ADR 0105). Extracted from
 * AssistantDrawer so both the full panel (which also owns a
 * conversation-list column) and the quick Mate sheet can host the same
 * thread over whatever page is behind it - neither owns any data itself,
 * both hand it a conversations/chat pair.
 *
 * Built on shadcn's chat primitives (MessageScroller/Message/Bubble/
 * Marker, ADR 0104): MessageScroller owns the scroll position and the
 * jump-to-latest button, Message/Bubble render one turn each, and Marker
 * carries the status line and tool activity while a reply is in flight.
 */
export function AssistantThread({ canWrite, conversations, chat, autoFocus, composerRef }: AssistantThreadProps) {
  const [content, setContent] = useState('')
  // [P1, ADR 0093/impeccable critique 2026-09-12] chat.abort() already
  // cancelled a request on unmount or a superseding send, but the operator
  // had no way to cancel a question themselves. Purely a local "did the
  // operator just stop this" flag - chat.error stays null through an abort,
  // so this is the only way to tell "stopped" apart from "idle before the
  // first question", and it clears the moment the next question is sent.
  const [stopped, setStopped] = useState(false)

  // ADR 0105 ("the answer outlives the page"): whenever the active
  // conversation becomes a real id - on mount, or when the operator
  // switches threads - rejoin whatever reply the server is still writing
  // for it. The only case to skip is this conversation's own send already
  // streaming (a brand new thread's first question creates the id and sends
  // straight away). A stream for a *different* conversation is superseded:
  // attach() closes it locally (its run carries on server-side) and clears
  // its status and draft, so this thread never shows, or Stops, another
  // conversation's run.
  const activeIdRef = useRef(conversations.activeId)
  activeIdRef.current = conversations.activeId
  useEffect(() => {
    const id = conversations.activeId
    if (id === null || chat.isStreamingConversation(id)) return
    let cancelled = false
    void (async () => {
      // A second effect run (React StrictMode's mount/cleanup/mount) simply
      // supersedes this call the same way a second chat.send() would - the
      // superseded attach resolves null and this local `cancelled` guard
      // keeps its resolution from doing anything further either way.
      const reply = await chat.attach(id)
      if (cancelled || reply === null || activeIdRef.current !== id) return
      conversations.appendLocal(reply)
      await conversations.refresh()
    })()
    return () => {
      cancelled = true
    }
    // Only an activeId change (including the initial mount) should rejoin a
    // run; re-running on every `chat`/`conversations` identity change would
    // loop or race an in-progress send.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversations.activeId])

  const handleSend = useCallback(async () => {
    const trimmed = content.trim()
    if (trimmed === '' || chat.sending || !canWrite) return
    setStopped(false)

    let conversationId = conversations.activeId
    if (conversationId === null) {
      conversationId = await conversations.create()
      if (conversationId === null) return
      // This thread now shows the new conversation, even before the render
      // that carries its id lands.
      activeIdRef.current = conversationId
    }

    conversations.appendLocal({
      id: `local-${Date.now()}`,
      conversationId,
      seq: -1,
      role: 'user',
      content: trimmed,
      createdAt: new Date().toISOString(),
    })
    setContent('')

    const reply = await chat.send(conversationId, trimmed)
    // The operator may have opened another thread while this one answered;
    // appendLocal writes into whichever thread is active now.
    if (reply && activeIdRef.current === conversationId) {
      conversations.appendLocal(reply)
      await conversations.refresh()
    }
  }, [content, chat, conversations, canWrite])

  const handleStop = useCallback(() => {
    void chat.abort()
    setStopped(true)
  }, [chat])

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void handleSend()
    }
  }

  const hasMessages = conversations.messages.length > 0
  // ADR 0105: the draft is the current round's answer text streaming in.
  // While it holds text, it is the thing on screen in place of the status
  // marker below - a retract (the round turned into tool calls) clears it
  // back to null and the marker returns, even though `sending` is still true.
  const showDraft = chat.sending && Boolean(chat.draft)
  const showStatusMarker = chat.sending && !showDraft

  return (
    <div
      className="mx-auto flex min-h-0 w-full max-w-3xl min-w-0 flex-1 flex-col gap-3"
      data-testid="assistant-thread-root"
    >
      <MessageScrollerProvider autoScroll defaultScrollPosition="last-anchor">
        <MessageScroller className="min-h-0 flex-1" data-testid="assistant-thread-message-scroller">
          <MessageScrollerViewport className="min-h-0 flex-1 px-1 py-2" data-testid="assistant-thread-scroll">
            <MessageScrollerContent className="gap-6">
              {!hasMessages && !showDraft ? (
                <p className="text-sm text-muted-foreground">Ask Mate: &ldquo;{EXAMPLE_QUESTION}&rdquo;</p>
              ) : (
                <>
                  {conversations.messages.map((message) => (
                    <MessageScrollerItem
                      key={message.id}
                      messageId={message.id}
                      scrollAnchor={message.role === 'user'}
                    >
                      {message.role === 'user' ? (
                        <Message align="end">
                          <MessageContent>
                            <Bubble variant="secondary" align="end">
                              <BubbleContent className="whitespace-pre-wrap">{message.content}</BubbleContent>
                            </Bubble>
                          </MessageContent>
                        </Message>
                      ) : (
                        <Message>
                          <MessageContent>
                            <Bubble variant="ghost">
                              <BubbleContent>
                                <AssistantMarkdown content={message.content} />
                              </BubbleContent>
                            </Bubble>
                            <MessageFooter
                              className="tabular-nums"
                              title={formatMessageFooterTitle(message)}
                            >
                              {formatMessageFooter(message)}
                            </MessageFooter>
                          </MessageContent>
                        </Message>
                      )}
                    </MessageScrollerItem>
                  ))}

                  {showDraft && (
                    <MessageScrollerItem messageId="draft">
                      <Message>
                        <MessageContent>
                          <Bubble variant="ghost">
                            <BubbleContent>
                              <AssistantMarkdown content={chat.draft ?? ''} />
                            </BubbleContent>
                          </Bubble>
                        </MessageContent>
                      </Message>
                    </MessageScrollerItem>
                  )}
                </>
              )}
            </MessageScrollerContent>
          </MessageScrollerViewport>
          <MessageScrollerButton />
        </MessageScroller>
      </MessageScrollerProvider>

      {chat.sending && (
        <Marker role={showStatusMarker ? 'status' : undefined}>
          {showStatusMarker && (
            <>
              <MarkerIcon>
                <Loader2 className="animate-spin" />
              </MarkerIcon>
              <MarkerContent className="shimmer">{chat.statusText ?? 'Thinking…'}</MarkerContent>
            </>
          )}
          {/* Stop outlives the status text: a streaming answer is still
              spending tokens and can be cut off mid-sentence. */}
          <Button variant="ghost" size="icon" className="ml-auto" aria-label="Stop asking" onClick={handleStop}>
            <Square className="h-4 w-4" />
          </Button>
        </Marker>
      )}

      {stopped && !chat.sending && <Marker className="text-[11px]">Stopped.</Marker>}

      {(chat.error || conversations.errorMessage) && (
        <Bubble variant="destructive">
          <BubbleContent>{chat.error ?? conversations.errorMessage}</BubbleContent>
        </Bubble>
      )}

      <div className="flex flex-col gap-1">
        <Textarea
          ref={composerRef}
          rows={3}
          placeholder="Ask about a passage, an anchorage, or how a panel works…"
          value={content}
          onChange={(event) => setContent(event.target.value)}
          onKeyDown={handleKeyDown}
          disabled={!canWrite}
          autoFocus={autoFocus}
        />
        {!canWrite && <p className="text-[11px] text-muted-foreground">Read-only session</p>}
        <div className="flex justify-end">
          <Button onClick={() => void handleSend()} disabled={chat.sending || !canWrite || content.trim() === ''}>
            Send
          </Button>
        </div>
      </div>
    </div>
  )
}
