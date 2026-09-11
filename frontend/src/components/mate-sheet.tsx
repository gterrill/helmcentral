import { useEffect, useRef } from 'react'

import { AssistantThread } from '@/components/assistant-thread'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useAssistantChat, type AssistantScreenContext } from '@/hooks/use-assistant-chat'
import { useAssistantConversations } from '@/hooks/use-assistant-conversations'

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
export function MateSheet({ open, onOpenChange, initialQuestion, screen, canWrite }: MateSheetProps) {
  const conversations = useAssistantConversations()
  const chat = useAssistantChat()

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
      }
    })()
  }, [open, initialQuestion, canWrite, screen, conversations, chat])

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex h-full min-w-0 flex-col gap-4 sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Mate</SheetTitle>
        </SheetHeader>
        <div className="min-h-0 min-w-0 flex-1">
          <AssistantThread canWrite={canWrite} conversations={conversations} chat={chat} autoFocus={!initialQuestion} />
        </div>
      </SheetContent>
    </Sheet>
  )
}
