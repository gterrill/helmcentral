import { Maximize2, MessageSquarePlus, Square } from 'lucide-react'
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

interface MateSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Set only when the sheet is opened from a voice question (ADR 0093
   * voice phase) - sent once, with `spoken: true`, as soon as the active
   * conversation is known. Absent for a plain "Ask Mate" open, which just
   * shows whatever thread is already current. */
  initialQuestion?: string
  screen: AssistantScreenContext
  canWrite: boolean
  /** Settings → Mate → "Read replies aloud" (ADR 0093 voice phase): when
   * on, the answer to a spoken question is read aloud as soon as it
   * arrives. */
  readAloud: boolean
  /** "Open in Mate" (ADR 0094) hands the sheet's current conversation - or
   * null, when none is active yet - to the caller, which is expected to
   * navigate to the full Mate panel and select it there. The sheet closes
   * itself right after; it does not wait for the panel to actually mount. */
  onOpenPanel: (conversationId: string | null) => void
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
export function MateSheet({ open, onOpenChange, initialQuestion, screen, canWrite, readAloud, onOpenPanel }: MateSheetProps) {
  const conversations = useAssistantConversations()
  const chat = useAssistantChat()
  const speechOutput = useSpeechOutput()
  const composerRef = useRef<HTMLTextAreaElement>(null)

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

  // "Open in Mate" (ADR 0094): hands the active conversation to the full
  // panel and closes the sheet. Not gated on chat.sending - a reply in
  // flight keeps running server side, and the panel shows it once that
  // thread loads there.
  const handleOpenInPanel = useCallback(() => {
    onOpenPanel(conversations.activeId)
    onOpenChange(false)
  }, [conversations.activeId, onOpenPanel, onOpenChange])

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
    if (sentQuestionRef.current === initialQuestion) return
    sentQuestionRef.current = initialQuestion

    void (async () => {
      let conversationId = conversations.activeId
      if (conversationId === null) {
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
  }, [open, initialQuestion, canWrite, screen, conversations, chat, readAloud, speechOutput])

  // Closing the sheet stops whatever it was reading - the operator has
  // moved on, and a voice answer trailing off after the panel that gave it
  // has vanished would be disorienting rather than helpful.
  useEffect(() => {
    if (!open) speechOutput.stop()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex h-full min-w-0 flex-col gap-4 sm:max-w-xl">
        <SheetHeader className="flex-row items-center justify-between space-y-0">
          <SheetTitle className="min-w-0 truncate">Mate</SheetTitle>
          {/* mr-6 clears the Sheet's own absolute-positioned close button
              (top-4 right-4) - the same offset the stop-reading button used
              on its own before these two joined it. */}
          <div className="mr-6 flex shrink-0 items-center gap-1">
            <Button variant="ghost" size="icon" aria-label="New conversation" onClick={handleNewConversation}>
              <MessageSquarePlus className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="icon" aria-label="Open in Mate" onClick={handleOpenInPanel}>
              <Maximize2 className="h-4 w-4" />
            </Button>
            {speechOutput.speaking && (
              <Button variant="ghost" size="icon" aria-label="Stop reading" onClick={() => speechOutput.stop()}>
                <Square className="h-4 w-4" />
              </Button>
            )}
          </div>
        </SheetHeader>
        <div className="min-h-0 min-w-0 flex-1">
          <AssistantThread
            canWrite={canWrite}
            conversations={conversations}
            chat={chat}
            autoFocus={!initialQuestion}
            composerRef={composerRef}
          />
        </div>
      </SheetContent>
    </Sheet>
  )
}
