import { ArrowUpRight, MessageSquarePlus, Square } from 'lucide-react'
import { useCallback, useEffect, useRef } from 'react'

import { AssistantThread } from '@/components/assistant-thread'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useAssistantChat, type AssistantScreenContext } from '@/hooks/use-assistant-chat'
import { useAssistantConversations } from '@/hooks/use-assistant-conversations'
import { useSpeechOutput } from '@/hooks/use-speech-output'
import { extractSpokenSummary } from '@/lib/spoken-summary'

// A full briefing can run well past a sentence or two; capped so a reply
// that somehow lacks the `## Spoken summary` heading (the backend contract
// - see hooks/use-assistant-chat.ts - is supposed to guarantee one whenever
// the question was spoken, but nothing enforces that client-side) still
// reads aloud as something short rather than the entire markdown document.
const SPOKEN_FALLBACK_LENGTH = 300

// Splits a describeLoadError() sentence into a headline (the first
// sentence) and detail (everything after it) for the recoverable-load-
// failure card below - the card puts the headline in full-strength text
// and the detail in muted text, rather than one undifferentiated block.
// A message with no second sentence (the plain "(HTTP <n>)" case) comes
// back with an empty detail, which the card simply omits.
function splitFirstSentence(message: string): { headline: string; detail: string } {
  const match = /^(.+?[.!?])(?:\s+(.*))?$/.exec(message)
  if (!match) return { headline: message, detail: '' }
  return { headline: match[1], detail: match[2] ?? '' }
}

interface MateSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Set only when the sheet is opened from a voice question (ADR 0093
   * voice phase) - sent once, with `spoken: true`, as soon as the active
   * conversation is known. Absent for a plain "Ask Mate" open, which just
   * shows whatever thread is already current. */
  initialQuestion?: string
  newConversation?: boolean
  screen: AssistantScreenContext
  canWrite: boolean
  /** Settings → Mate → "Read replies aloud" (ADR 0093 voice phase): when
   * on, the answer to a spoken question is read aloud as soon as it
   * arrives. */
  readAloud: boolean
  /** "Open the Mate page" (ADR 0094, labelled "Open in Mate" there) hands the
   * sheet's current conversation - or null, when none is active yet - to the
   * caller, which is expected to navigate to the full Mate panel and select
   * it there. The sheet closes itself right after; it does not wait for the
   * panel to actually mount. */
  onOpenPanel: (conversationId: string | null) => void
  /** Mirrors AssistantDrawer's own prop of the same name (mate-answer-toast
   * plan): fires whenever the sheet's active conversation settles, so
   * App.tsx can tell whether a given conversation is the one the sheet is
   * currently showing - the App-level answer watcher needs this to know
   * not to toast for a reply the sheet is displaying right now. */
  onActiveConversationChange?: (id: string | null) => void
}

/**
 * The quick channel for Mate (ADR 0093 voice phase): a right-hand sheet
 * that hosts the same AssistantThread the full panel uses, over whatever
 * page is behind it, so a voice question doesn't have to leave the
 * Forecast panel (or any other) to get answered. Mounted once in the shell
 * with its own conversations/chat state - independent of the panel's - so
 * the sheet's thread survives being closed and reopened the same way the
 * panel's does.
 */
export function MateSheet({ open, onOpenChange, initialQuestion, newConversation = false, screen, canWrite, readAloud, onOpenPanel, onActiveConversationChange }: MateSheetProps) {
  const conversations = useAssistantConversations()
  const chat = useAssistantChat()
  const speechOutput = useSpeechOutput()
  const composerRef = useRef<HTMLTextAreaElement>(null)

  // Same pattern as AssistantDrawer's own effect of the same name: waits on
  // `loading` so a transient null (before the mount fetch has resolved)
  // never reaches the caller as "no active conversation".
  useEffect(() => {
    if (conversations.loading) return
    onActiveConversationChange?.(conversations.activeId)
  }, [onActiveConversationChange, conversations.activeId, conversations.loading])

  // "New conversation" (ADR 0094): the sheet is one thread plus the
  // composer, and it keeps appending to the current conversation - no
  // time-based expiry - until the operator explicitly asks for a fresh one
  // here. create() both creates and selects the new conversation; focusing
  // the composer straight after is what autoFocus alone can't do, since that
  // only ever fires on mount.
  const handleNewConversation = useCallback(() => {
    void (async () => {
      await conversations.create()
      composerRef.current?.focus()
    })()
  }, [conversations])

  // "Open the Mate page" (ADR 0094): hands the active conversation to the
  // full panel and closes the sheet. Not gated on chat.sending - a reply in
  // flight keeps running server side, and the panel shows it once that
  // thread loads there.
  const handleOpenInPanel = useCallback(() => {
    onOpenPanel(conversations.activeId)
    onOpenChange(false)
  }, [conversations.activeId, onOpenPanel, onOpenChange])

  // The recoverable-load-failure card's "Open the Mate page" (impeccable
  // critique 2026-09-12, P0): a failed initial load never got an activeId,
  // so unlike handleOpenInPanel above this always hands the full panel
  // null rather than a conversation that was never selected - the panel
  // falls back to its own newest-thread selection from there.
  const handleOpenPanelFromErrorCard = useCallback(() => {
    onOpenPanel(null)
    onOpenChange(false)
  }, [onOpenPanel, onOpenChange])

  // Refetches every time the sheet opens, rather than once per app session
  // (impeccable critique 2026-09-12, P0): the sheet has no conversation
  // list of its own, so a load that failed while the sheet was last open
  // otherwise had no way to recover short of reloading the whole page.
  // Cheap to repeat - the sheet always opens onto the newest thread anyway.
  useEffect(() => {
    if (open) void conversations.reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Sends `initialQuestion` exactly once per open. Waits on
  // conversations.loading so it doesn't create a fresh conversation before
  // the hook's own mount effect has had a chance to select the most
  // recently active one - "creates a conversation if none is active" means
  // none active once that's actually known, not before.
  const sentQuestionRef = useRef<string | null>(null)
  useEffect(() => {
    if (!open) {
      sentQuestionRef.current = null
      return
    }
    if (!initialQuestion || !canWrite || conversations.loading) return

    const key = `${newConversation ? 'new:' : 'existing:'}${initialQuestion}`
    if (sentQuestionRef.current === key) return
    sentQuestionRef.current = key

    void (async () => {
      let conversationId = conversations.activeId
      if (newConversation || conversationId === null) {
        conversationId = await conversations.create()
        if (conversationId === null) return
      }

      conversations.appendLocal({
        id: `local-${Date.now()}`,
        conversationId,
        seq: -1,
        role: 'user',
        content: initialQuestion,
        createdAt: new Date().toISOString(),
      })

      const reply = await chat.send(conversationId, initialQuestion, { spoken: true, screen })
      if (reply) {
        conversations.appendLocal(reply)
        await conversations.refresh()
        // Read-aloud (ADR 0093 voice phase): only for a reply to a question
        // that came in by voice - the backend's `## Spoken summary` section
        // only exists because `spoken: true` asked for it, so this only
        // ever runs alongside that same condition.
        if (readAloud) {
          speechOutput.speak(extractSpokenSummary(reply.content) ?? reply.content.slice(0, SPOKEN_FALLBACK_LENGTH))
        }
      }
    })()
  }, [open, initialQuestion, newConversation, canWrite, screen, conversations, chat, readAloud, speechOutput])

  // Closing the sheet stops whatever it was reading - the operator has
  // moved on, and a voice answer trailing off after the panel that gave it
  // has vanished would be disorienting rather than helpful.
  useEffect(() => {
    if (!open) speechOutput.stop()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const activeConversationTitle =
    conversations.conversations.find((conversation) => conversation.id === conversations.activeId)?.title ?? null

  // A failed load with nothing already on screen (impeccable critique
  // 2026-09-12, P0): the sheet has no conversation list of its own to fall
  // back on, so this is the only signal the operator gets that anything
  // went wrong. Once at least one conversation has loaded, a later error
  // falls through to AssistantThread's own error row instead - the thread
  // is worth keeping visible at that point.
  const loadError =
    conversations.errorMessage !== null && conversations.conversations.length === 0
      ? splitFirstSentence(conversations.errorMessage)
      : null

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex h-full min-w-0 flex-col gap-4 sm:max-w-xl">
        {/* pr-10 reserves room for the primitive's own absolute-positioned
            40 px close button (top-2 right-2) structurally, rather than a
            hand-tuned margin on the actions row next to it. */}
        <SheetHeader className="flex-row items-center justify-between space-y-0 pr-10">
          <div className="min-w-0">
            {/* text-base matches DESIGN.md's Title token (1rem); the primitive's
                own default is text-lg (18px). */}
            <SheetTitle className="truncate text-base">Mate</SheetTitle>
            {/* Which thread this is (impeccable critique 2026-09-12, weak
                scent - "Mate" alone doesn't say which of several open
                conversations the sheet is showing). `--` before any thread
                is selected, same as every other zero-state in this app. */}
            <div className="truncate text-[11px] text-muted-foreground">
              {activeConversationTitle ?? '--'}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              aria-label="New conversation"
              title="New conversation"
              onClick={handleNewConversation}
            >
              <MessageSquarePlus className="h-4 w-4" />
            </Button>
            {/* ArrowUpRight over PanelRightOpen: this navigates away to a
                different page entirely (and closes the sheet behind it),
                not a panel toggling open in place, so the "go to" arrow
                reads truer than a panel glyph here. */}
            <Button
              variant="ghost"
              size="icon"
              aria-label="Open the Mate page"
              title="Open the Mate page"
              onClick={handleOpenInPanel}
            >
              <ArrowUpRight className="h-4 w-4" />
            </Button>
            {speechOutput.speaking && (
              <Button
                variant="ghost"
                size="icon"
                aria-label="Stop reading"
                title="Stop reading"
                onClick={() => speechOutput.stop()}
              >
                <Square className="h-4 w-4" />
              </Button>
            )}
          </div>
        </SheetHeader>
        {/* flex + flex-col here is load-bearing, not decorative: AssistantThread's
            own root is `flex-1 min-h-0 flex-col`, which only sizes against a flex
            parent. This wrapper used to be a plain block `<div>`, so that chain had
            nothing to size against, the message list's `overflow-y-auto` never got
            a bounded height, and a long reply pushed the composer off the bottom of
            the viewport (impeccable critique 2026-09-12, P0). */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="mate-sheet-thread-region">
          {loadError ? (
            <div className="space-y-3 rounded-lg border border-border bg-card p-4">
              <p className="text-sm text-foreground">{loadError.headline}</p>
              {loadError.detail !== '' && <p className="text-sm text-muted-foreground">{loadError.detail}</p>}
              <div className="flex gap-2">
                <Button onClick={() => void conversations.reload()}>Try again</Button>
                <Button variant="ghost" onClick={handleOpenPanelFromErrorCard}>
                  Open the Mate page
                </Button>
              </div>
            </div>
          ) : (
            <AssistantThread
              canWrite={canWrite}
              conversations={conversations}
              chat={chat}
              autoFocus={!initialQuestion}
              composerRef={composerRef}
            />
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
