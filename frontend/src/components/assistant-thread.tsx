import { ArrowUp, Loader2, NotebookPen, Paperclip, Square, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type DragEvent, type KeyboardEvent, type Ref } from 'react'
import { toast } from 'sonner'

import { AssistantMarkdown } from '@/components/assistant-markdown'
import { Bubble, BubbleContent } from '@/components/ui/bubble'
import { Button } from '@/components/ui/button'
import { DictateButton, DictationError, DictationStatus, useDictation } from '@/components/dictation'
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupTextarea } from '@/components/ui/input-group'
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
import type { useAssistantChat } from '@/hooks/use-assistant-chat'
import type { AssistantMessage, AssistantMessageAttachment, useAssistantConversations } from '@/hooks/use-assistant-conversations'
import { useDocumentUploads, type StagedDocument } from '@/hooks/use-document-uploads'
import { useNotes } from '@/hooks/use-notes'
import { documentViewerHref } from '@/lib/document-citation'
import { cn } from '@/lib/utils'

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

// ADR 0106 F2: a short, human label for a staged attachment's current
// state, next to its filename. "Reading…" covers `pending` - the backend
// field is `status`, but the operator never needs to know the difference
// between still-extracting and still-enriching, only that it isn't done.
function stagedDocumentStatusLabel(item: StagedDocument): string {
  switch (item.status) {
    case 'uploading':
      return `${item.progress}%`
    case 'pending':
      return 'Reading…'
    case 'indexed':
      return 'Indexed'
    case 'failed':
      return item.error ?? 'Failed'
  }
}

function formatMessageFooterTitle(message: AssistantMessage): string {
  const model = message.model && message.model !== '' ? message.model : '--'
  const tokens =
    typeof message.promptTokens === 'number' && typeof message.completionTokens === 'number'
      ? (message.promptTokens + message.completionTokens).toLocaleString()
      : '--'
  return `${model} · ${tokens} tokens`
}

// ADR 0106 F1: the small filename chips shown under a past user message's
// bubble, now a plain link into the Documents panel's viewer for that
// document (F1's own deep-link query param - app-location.ts's
// documentId/formatAppLocation) rather than inert text. An ordinary <a
// href>, not a client-side navigate: this component has no reach into
// App.tsx's panel state, and "keep it simple" (the plan's own words for
// this wiring) means a real link the browser handles on its own, the same
// as the collision-tuning link (lib/collision-tuning.ts) does for an
// external URL. documentViewerHref itself now lives in
// lib/document-citation.ts (Mate UI cycle: document sources as icons) so the
// citation link renderer in assistant-markdown-impl.tsx can recognise this
// exact same href shape rather than growing a second copy of it.

function MessageAttachmentChips({ attachments }: { attachments: AssistantMessageAttachment[] }) {
  return (
    <div className="flex flex-wrap justify-end gap-1">
      {attachments.map((attachment) => (
        <a
          key={attachment.documentId}
          href={documentViewerHref(attachment.documentId)}
          className="max-w-40 truncate rounded-md border border-border bg-muted/50 px-2 py-0.5 text-[11px] text-muted-foreground hover:border-primary/40 hover:text-primary"
          title={attachment.filename}
        >
          {attachment.filename}
        </a>
      ))}
    </div>
  )
}

/**
 * Saves one of Mate's answers as a note, verbatim.
 *
 * This is what replaced a `draft_note` tool. A tool would have had to
 * either write (breaking runToolRound's read-only contract, which is the
 * justification for running a round's calls concurrently without locking)
 * or hand back a draft plus an id for the UI to save - and a retried or
 * cancelled call would produce duplicate notes with no idempotency key,
 * which `sha256 UNIQUE` cannot catch because two drafts of the same text
 * get different UUIDs. A button on a message that already exists has
 * neither problem, and Mate still writes nothing.
 *
 * The body is the answer as-is. The backend derives a title from the first
 * line and classifies it (classifyNoteType, no network), so this asks the
 * operator for nothing at the moment they are least inclined to answer.
 */
function SaveAsNoteButton({ content }: { content: string }) {
  const notes = useNotes()
  const [saving, setSaving] = useState(false)

  const save = useCallback(async () => {
    const body = content.trim()
    if (body === '') return
    setSaving(true)
    try {
      await notes.createNote({ body })
      toast('Saved to Notes. It is unfiled until you put it somewhere.')
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message, verbatim.
      toast.error(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [content, notes])

  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      disabled={saving}
      onClick={() => { void save() }}
    >
      <NotebookPen className="h-3.5 w-3.5" data-icon="inline-start" aria-hidden="true" />
      Save as note
    </Button>
  )
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

  // ADR 0106 F2: the composer's staged attachments. One useDocumentUploads()
  // instance per AssistantThread - it isn't threaded through as a prop
  // because nothing outside the composer needs it, the same way `content`
  // above is local rather than owned by AssistantDrawer/MateSheet.
  const uploads = useDocumentUploads()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [dragOver, setDragOver] = useState(false)

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

  // ADR 0122: dictation, not push-to-talk - appends to the composer, never
  // sends by itself. The header's "Talk to Mate" mic (App.tsx) stays the
  // only voice control that sends on its own. Declared ahead of handleSend
  // below (rather than where it's used further down, near handleKeyDown)
  // because handleSend now cancels it at send time.
  const dictation = useDictation({ setValue: setContent })

  const handleSend = useCallback(async () => {
    const trimmed = content.trim()
    // ADR 0106 F2: a question may now be nothing but an attachment - the
    // backend accepts empty content as long as at least one document is
    // attached - so the old "no text, nothing to send" guard has to allow
    // that case through. `uploads.ready` is what actually gates Send on
    // upload/indexing state; see use-document-uploads.ts for exactly what
    // it requires.
    const attachmentIds = uploads.items
      .map((item) => item.documentId)
      .filter((id): id is string => id !== null)
    if ((trimmed === '' && attachmentIds.length === 0) || chat.sending || !canWrite || !uploads.ready) return
    setStopped(false)

    let conversationId = conversations.activeId
    if (conversationId === null) {
      conversationId = await conversations.create()
      if (conversationId === null) return
      // This thread now shows the new conversation, even before the render
      // that carries its id lands.
      activeIdRef.current = conversationId
    }

    const attachmentChips: AssistantMessageAttachment[] = uploads.items.map((item) => ({
      documentId: item.documentId as string,
      filename: item.filename,
    }))

    conversations.appendLocal({
      id: `local-${Date.now()}`,
      conversationId,
      seq: -1,
      role: 'user',
      content: trimmed,
      createdAt: new Date().toISOString(),
      attachments: attachmentChips.length > 0 ? attachmentChips : undefined,
    })
    // Code review: sending used to leave an active dictation running - it
    // kept listening after the composer was cleared below, so a word
    // recognised after Send landed in the now-empty box as though it were
    // the start of a fresh, unsent message. Cancelled here, before the
    // clear, so nothing further can land in it.
    dictation.cancel()
    setContent('')

    // Only included when something is actually staged - same
    // conditional-inclusion `send()` already applies to `spoken`/`screen`,
    // so a plain text-only question posts exactly the body it always has.
    const reply =
      attachmentIds.length > 0
        ? await chat.send(conversationId, trimmed, { attachments: attachmentIds })
        : await chat.send(conversationId, trimmed)

    // A successful send has nothing left for the composer to hold onto -
    // clear() aborts nothing (everything staged already finished
    // uploading, since uploads.ready gated Send above) and just drops the
    // now-sent chips. A failed send (reply === null, chat.error is set)
    // leaves them staged so the operator can retry without re-uploading.
    if (reply) uploads.clear()
    // The operator may have opened another thread while this one answered;
    // appendLocal writes into whichever thread is active now.
    if (reply && activeIdRef.current === conversationId) {
      conversations.appendLocal(reply)
      await conversations.refresh()
    }
  }, [content, chat, conversations, canWrite, uploads, dictation])

  const handleStop = useCallback(() => {
    void chat.abort()
    setStopped(true)
  }, [chat])

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void handleSend()
      return
    }
    dictation.handleFieldKeyDown(event)
  }

  // ADR 0106 F2: the paperclip button never touches the DOM file input
  // directly - it's hidden (getByTestId'd in tests, not a visible control)
  // and only ever driven by ref.click() here or by a drop below.
  const handleFilesChosen = useCallback((files: FileList | null) => {
    if (files && files.length > 0) uploads.add(files)
    // Clears the input's own value so choosing the exact same file again
    // later still fires a change event - the browser otherwise treats an
    // unchanged file list as nothing having changed.
    if (fileInputRef.current) fileInputRef.current.value = ''
  }, [uploads])

  const handleDragOver = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragOver(true)
  }, [])

  const handleDragLeave = useCallback(() => setDragOver(false), [])

  const handleDrop = useCallback((event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setDragOver(false)
    if (event.dataTransfer.files.length > 0) uploads.add(event.dataTransfer.files)
  }, [uploads])

  const hasMessages = conversations.messages.length > 0
  // ADR 0105: the draft is the current round's answer text streaming in.
  // While it holds text, it is the thing on screen in place of the status
  // marker below - a retract (the round turned into tool calls) clears it
  // back to null and the marker returns, even though `sending` is still true.
  const showDraft = chat.sending && Boolean(chat.draft)
  const showStatusMarker = chat.sending && !showDraft

  // ADR 0106 F2: Send's own disabled reasons, beyond the plain "nothing
  // typed and nothing attached" case. uploads.ready requires every staged
  // item to be indexed or failed - so this covers both a chip mid-upload
  // (no document id yet) and one already uploaded but still being read or
  // enriched (`pending`). Reported as a visible line below, not just a
  // hover title, since this is exactly the kind of state a touchscreen
  // operator at the helm would otherwise have no way to discover.
  const attachmentsNotReady = !uploads.ready
  const attachmentWaitMessage = uploads.items.some((item) => item.status === 'uploading')
    ? 'Waiting for attachments to finish uploading…'
    : attachmentsNotReady
      ? 'Waiting for attachments to finish reading…'
      : null
  const hasContentOrAttachment = content.trim() !== '' || uploads.items.length > 0
  const sendDisabled = chat.sending || !canWrite || !hasContentOrAttachment || attachmentsNotReady

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
                            {message.content !== '' && (
                              <Bubble variant="secondary" align="end">
                                <BubbleContent className="whitespace-pre-wrap">{message.content}</BubbleContent>
                              </Bubble>
                            )}
                            {/* ADR 0106 F2: attachments on a message that was
                                sent with none, or content-less, still need
                                somewhere to render - see MessageAttachmentChips. */}
                            {message.attachments && message.attachments.length > 0 && (
                              <MessageAttachmentChips attachments={message.attachments} />
                            )}
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
                            <div className="flex items-center gap-2">
                              <MessageFooter
                                className="tabular-nums"
                                title={formatMessageFooterTitle(message)}
                              >
                                {formatMessageFooter(message)}
                              </MessageFooter>
                              {/* The small answer to what a draft_note tool
                                  would have done (plan phase 6, cut): Mate
                                  never writes anything, the operator's tap
                                  does. No new tool, so runToolRound's
                                  read-only contract stays intact, and no
                                  chance of a retried tool call duplicating
                                  a note.
                                  Creates the note outright rather than
                                  opening the capture sheet: Mate is often
                                  itself a sheet, and stacking two is worse
                                  than saving verbatim and trimming later
                                  from Documents - which is what capture is
                                  for anyway. */}
                              {canWrite && (
                                <SaveAsNoteButton content={message.content} />
                              )}
                            </div>
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

      {/* [shadcn 2026-06 chat composer] attach and send used to sit in a
          plain row below the textarea; this rebuilds the composer as one
          InputGroup panel - the bordered, focus-ringed shape upstream's
          chat components changelog ships - with both buttons living inside
          it as a block-end addon, the same way upstream's own demo wires
          PlusIcon/ArrowUpIcon. The drop target and its dragOver ring stay
          on this outer wrapper rather than moving onto InputGroup, since a
          file can be dropped anywhere over the composer, not just the
          panel itself. */}
      <div
        className={cn(
          'flex flex-col gap-1 rounded-md',
          // ADR 0106 F2: a visible drop state - dragging a file over the
          // whole composer (not just some narrow drop target) is the
          // discoverable affordance; the ring makes the target obvious
          // before the operator commits to releasing the file.
          dragOver && 'ring-2 ring-primary ring-offset-2 ring-offset-background',
        )}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        data-testid="composer-dropzone"
      >
        <InputGroup>
          {uploads.items.length > 0 && (
            // block-start puts the staged chips inside the panel, above the
            // textarea, rather than floating above it as a separate block -
            // flex-wrap because chips wrap onto more than one line once a
            // few are staged.
            <InputGroupAddon align="block-start" className="flex-wrap" data-testid="composer-attachments">
              {uploads.items.map((item) => (
                <div
                  key={item.key}
                  className={cn(
                    'flex items-center gap-1.5 rounded-md border px-2 py-1 text-[11px]',
                    item.status === 'failed' ? 'border-destructive/40 bg-destructive/10' : 'border-border bg-muted/50',
                  )}
                >
                  {item.status === 'uploading' && <Loader2 className="h-3 w-3 shrink-0 animate-spin" />}
                  <span className="max-w-40 truncate" title={item.filename}>
                    {item.filename}
                  </span>
                  <span
                    className={cn(
                      'tabular-nums',
                      item.status === 'failed' ? 'text-destructive' : 'text-muted-foreground',
                    )}
                  >
                    {stagedDocumentStatusLabel(item)}
                  </span>
                  <button
                    type="button"
                    aria-label={`Remove ${item.filename}`}
                    onClick={() => uploads.remove(item.key)}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              ))}
            </InputGroupAddon>
          )}

          <InputGroupTextarea
            ref={composerRef}
            rows={3}
            placeholder="Ask about a passage, an anchorage, or how a panel works…"
            value={content}
            onChange={(event) => setContent(event.target.value)}
            onKeyDown={handleKeyDown}
            disabled={!canWrite}
            autoFocus={autoFocus}
          />

          <InputGroupAddon align="block-end">
            <InputGroupButton
              aria-label="Attach files"
              title="Attach files"
              size="icon-sm"
              disabled={!canWrite}
              onClick={() => fileInputRef.current?.click()}
            >
              <Paperclip className="h-4 w-4" />
            </InputGroupButton>
            {/* ADR 0122: dictation appends to the composer and never sends -
                DictateButton renders nothing at all with no speech API, and
                DictationStatus (the visible "Listening…"/interim line) only
                renders while actually listening, so neither one changes
                this addon's layout otherwise. */}
            <DictateButton dictation={dictation} disabled={!canWrite} />
            <DictationStatus dictation={dictation} />
            {/* Send is an icon button here, matching upstream's ArrowUpIcon
                composer control - ml-auto is what actually pushes it to the
                far edge, since the addon itself packs its children to the
                start. The sr-only span is load-bearing: it is the button's
                whole accessible name ("Send"), not decoration - tests and
                assistive tech both read this button by that name. */}
            <InputGroupButton
              variant="default"
              size="icon-sm"
              className="ml-auto"
              disabled={sendDisabled}
              onClick={() => void handleSend()}
            >
              <ArrowUp className="h-4 w-4" />
              <span className="sr-only">Send</span>
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>

        {uploads.error && (
          <p className="text-[11px] text-destructive" role="alert">
            {uploads.error}
          </p>
        )}
        <DictationError dictation={dictation} />
        {!canWrite && <p className="text-[11px] text-muted-foreground">Read-only session</p>}
        {/* Send's disabled-attachment reason is a visible line, not just a
            hover title: the operator most likely to hit this is on the wall
            kiosk or a touchscreen helm, where nothing hovers. */}
        {canWrite && attachmentWaitMessage && (
          <p className="text-[11px] text-muted-foreground">{attachmentWaitMessage}</p>
        )}
        <input
          ref={fileInputRef}
          type="file"
          multiple
          className="hidden"
          aria-label="Choose files to attach"
          data-testid="composer-file-input"
          onChange={(event) => handleFilesChosen(event.target.files)}
        />
      </div>
    </div>
  )
}
