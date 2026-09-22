import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent } from '@/components/ui/popover'

// Written once, per the plan: an empty send has to mean *something*, and
// this is what it means everywhere this component is used - Mate decides
// what's appropriate for the passage (define it, explain it) and, only if
// it makes a factual claim about this boat, checks that claim against the
// boat's own documents before repeating it back.
export const DEFAULT_ASK_MATE_QUESTION =
  "Explain this passage. If it makes a factual claim about this boat, check it against the boat's " +
  "own documents (the manual, and the drawings and price lists in the Builder folder) and cite your sources."

// A selection this long is almost certainly the operator dragging across a
// whole section rather than a phrase they want explained - capped so the
// message to Mate (and the context window it eats into) stays bounded, but
// never silently: buildAskMateSelectionMessage always marks a clipped quote.
const MAX_QUOTE_LENGTH = 1500
const TRUNCATION_NOTE = 'selection truncated to 1,500 characters'

// Markdown blockquote: every line of the (possibly clipped) quote gets its
// own "> " prefix, including blank lines (a bare "> "), so a multi-paragraph
// selection still reads as one quoted block rather than breaking out of it
// on its first blank line.
function toBlockquote(text: string): string {
  return text
    .split('\n')
    .map((line) => (line.length > 0 ? `> ${line}` : '>'))
    .join('\n')
}

/**
 * The message body sent to Mate for a "Ask Mate about this" selection: the
 * quote (as a blockquote, truncated and marked past MAX_QUOTE_LENGTH), the
 * source note's title and id (read_document takes a plain id - see
 * backend/assistant_tools.go - so no hc-note:/hc-doc: scheme is needed
 * here), and the operator's question or DEFAULT_ASK_MATE_QUESTION.
 * Exported for direct unit testing of the formatting rules, independent of
 * the selection/popover plumbing around it.
 */
export function buildAskMateSelectionMessage(quote: string, noteTitle: string, noteId: string, question: string): string {
  const trimmedQuestion = question.trim() === '' ? DEFAULT_ASK_MATE_QUESTION : question.trim()
  const truncated = quote.length > MAX_QUOTE_LENGTH
  const clipped = truncated ? quote.slice(0, MAX_QUOTE_LENGTH) : quote
  const quoteBlock = truncated ? `${toBlockquote(clipped)}\n> … (${TRUNCATION_NOTE})` : toBlockquote(clipped)
  return `${quoteBlock}\n\nFrom "${noteTitle}" (document id: ${noteId}).\n\n${trimmedQuestion}`
}

interface SelectionState {
  quote: string
  /** The cloned Range the selection covered when it settled - cloned (not
   * the live Selection's own Range) so it stays put if the DOM selection
   * itself changes later, but still read live for position (see
   * createSelectionAnchor below) rather than frozen to a rect snapshot. */
  range: Range
  /** Only a mouse-originated selection autofocuses the input - a touch
   * selection must not pop the on-screen keyboard uninvited; the operator
   * taps the input themselves when they want to type. */
  autoFocusInput: boolean
}

// The minimal shape createSelectionAnchor actually needs - a real Range
// satisfies this structurally, and a test can hand it a lightweight fake
// without constructing (and connecting, and laying out) a real DOM Range.
interface RangeLike {
  startContainer: Node
  endContainer: Node
  getBoundingClientRect(): DOMRect
}

function isRangeLikeConnected(range: RangeLike): boolean {
  return range.startContainer.isConnected && range.endContainer.isConnected
}

/**
 * A Popover "virtual element" anchor (Base UI's `anchor` prop) that reads
 * the given Range's own getBoundingClientRect() live on every call, rather
 * than a rect captured once at selection time - so the popover tracks the
 * reading pane scrolling or resizing (floating-ui re-measures the anchor on
 * both) instead of drifting away from the text it was anchored to.
 *
 * Calls `onStale` - instead of quietly floating at a wrong or vanished spot -
 * the moment the range's own boundary nodes fall out of the document (the
 * note re-rendered, replacing this DOM subtree), or its rect goes from
 * having real size to none (same cause, different symptom). A rect that is
 * zero-size on its very FIRST read does NOT count: that's the ordinary case
 * under a test environment with no real layout engine, not a stale range,
 * so treating it as staleness here would make every popover self-dismiss
 * the instant it opened in this project's own test suite.
 */
export function createSelectionAnchor(range: RangeLike, onStale: () => void): { getBoundingClientRect: () => DOMRect } {
  let sawRealRect = false
  return {
    getBoundingClientRect: () => {
      if (!isRangeLikeConnected(range)) {
        onStale()
        return new DOMRect()
      }
      const rect = range.getBoundingClientRect()
      if (rect.width > 0 || rect.height > 0) {
        sawRealRect = true
      } else if (sawRealRect) {
        onStale()
      }
      return rect
    },
  }
}

export interface AskMateSelectionProps {
  /** The note (or other markdown document) this rendered body belongs to -
   * threaded into the message so Mate can open the source itself with
   * read_document. */
  noteId: string
  noteTitle: string
  /** Whether Mate is configured and reachable right now (the same status
   * AssistantDrawer's "problem" card already gates its own composer on,
   * hooks/use-assistant-status.ts) - false hides this feature entirely
   * rather than offering a control that would just fail. */
  mateAvailable: boolean
  /** Matches App.tsx's openMate signature exactly - always called with
   * `{ newConversation: true }`, per the plan ("Always start a new
   * conversation"). Optional so a caller that hasn't wired Mate up yet (or
   * a test with nothing to assert on) doesn't have to pass a no-op. */
  onAskMate?: (question: string, options?: { newConversation?: boolean }) => void
  children: ReactNode
  className?: string
}

/**
 * Wraps a rendered note body (the manual reading pane, the note viewer) and
 * offers "Ask Mate about this" for whatever the operator selects inside it.
 * Selections elsewhere on the page - or with Mate not available at all -
 * never show anything: this is additive chrome over the note body, never a
 * requirement to read it.
 */
export function AskMateSelection({ noteId, noteTitle, mateAvailable, onAskMate, children, className }: AskMateSelectionProps) {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const popupRef = useRef<HTMLDivElement | null>(null)
  const [selection, setSelection] = useState<SelectionState | null>(null)
  const [question, setQuestion] = useState('')
  // The most recent pointerdown's type, read (not reacted to) the moment a
  // selection settles - there is no pointer info on a `selectionchange`
  // event itself, so this is the only way to tell a mouse drag-select from
  // a touch one by the time it fires.
  const lastPointerTypeRef = useRef<string | null>(null)
  // Whether the most recent pointerdown anywhere on the page landed inside
  // the popover itself - see isInteractingWithPopup below. Reset on dismiss
  // so a stale true from a past interaction can never suppress a real
  // collapse against a LATER popover instance.
  const pointerDownInPopupRef = useRef(false)

  const dismiss = useCallback(() => {
    setSelection(null)
    setQuestion('')
    pointerDownInPopupRef.current = false
  }, [])

  // Selection tracking is entirely off, not merely hidden, while Mate isn't
  // available - no listener means no popover can ever appear, matching the
  // plan ("don't show the popover at all").
  useEffect(() => {
    if (!mateAvailable) return

    // Whether the operator is currently interacting with the popover itself
    // (bug: focusing - or on touch, merely pointing down on - its input can
    // collapse the browser's own document Selection, which used to read
    // exactly like the operator clicking away and dismiss the popover
    // before anyone could type into it). Checked at the moment a collapse
    // is seen, not cached, so it reflects whichever came last: a pointerdown
    // on the popup, or focus already landing there.
    function isInteractingWithPopup(): boolean {
      if (pointerDownInPopupRef.current) return true
      const active = document.activeElement
      return active !== null && popupRef.current !== null && popupRef.current.contains(active)
    }

    function handlePointerDown(event: PointerEvent) {
      lastPointerTypeRef.current = event.pointerType
      pointerDownInPopupRef.current =
        event.target instanceof Node && popupRef.current !== null && popupRef.current.contains(event.target)
    }

    function handleSelectionChange() {
      const container = containerRef.current
      const domSelection = document.getSelection()
      if (!container || !domSelection || domSelection.isCollapsed || domSelection.rangeCount === 0) {
        if (isInteractingWithPopup()) return
        setSelection(null)
        return
      }
      const text = domSelection.toString()
      if (text.trim() === '') {
        if (isInteractingWithPopup()) return
        setSelection(null)
        return
      }
      const range = domSelection.getRangeAt(0)
      // Fully inside the note body: a range that starts or ends outside
      // `container` shares a commonAncestorContainer that isn't `container`
      // (or one of its descendants), so this also rejects a selection that
      // merely overlaps the edge of the note rather than sitting inside it.
      if (!container.contains(range.commonAncestorContainer)) {
        setSelection(null)
        return
      }
      setSelection({
        quote: text,
        // Cloned, not the live Range getRangeAt(0) hands back - this needs
        // to keep pointing at the same text after the Selection itself
        // later changes (e.g. collapses into the popover's input).
        range: range.cloneRange(),
        autoFocusInput: lastPointerTypeRef.current === 'mouse',
      })
    }

    document.addEventListener('pointerdown', handlePointerDown, true)
    document.addEventListener('selectionchange', handleSelectionChange)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      document.removeEventListener('selectionchange', handleSelectionChange)
    }
  }, [mateAvailable])

  // A click truly outside both the note body and the popover itself - the
  // ordinary case (clicking elsewhere in the note, or on the page) already
  // collapses the DOM selection and is caught by handleSelectionChange
  // above, but a click on inert chrome (the sidebar, a button that owns no
  // text) doesn't necessarily fire `selectionchange`, so this is a second,
  // independent path to the same dismiss.
  useEffect(() => {
    if (!selection) return
    function handlePointerDownOutside(event: PointerEvent) {
      const target = event.target as Node | null
      if (!target) return
      if (containerRef.current?.contains(target)) return
      if (popupRef.current?.contains(target)) return
      dismiss()
    }
    document.addEventListener('pointerdown', handlePointerDownOutside, true)
    return () => document.removeEventListener('pointerdown', handlePointerDownOutside, true)
  }, [selection, dismiss])

  const anchor = useMemo(() => {
    if (!selection) return undefined
    return createSelectionAnchor(selection.range, dismiss)
  }, [selection, dismiss])

  const handleSend = useCallback(() => {
    if (!selection) return
    const message = buildAskMateSelectionMessage(selection.quote, noteTitle, noteId, question)
    onAskMate?.(message, { newConversation: true })
    // The operator has moved on to Mate; leave neither the popover nor the
    // browser's own text selection lingering over the note body.
    document.getSelection()?.removeAllRanges()
    dismiss()
  }, [selection, question, noteTitle, noteId, onAskMate, dismiss])

  const handleKeyDown = useCallback((event: React.KeyboardEvent) => {
    if (event.key === 'Enter') {
      event.preventDefault()
      handleSend()
    } else if (event.key === 'Escape') {
      event.stopPropagation()
      dismiss()
    }
  }, [handleSend, dismiss])

  return (
    <div ref={containerRef} className={className}>
      {children}
      {mateAvailable && (
        <Popover open={selection !== null} onOpenChange={(open) => { if (!open) dismiss() }}>
          {selection && (
            <PopoverContent
              ref={popupRef}
              anchor={anchor}
              side="top"
              align="center"
              className="w-80 max-w-[calc(100vw-1rem)] p-2"
            >
              <div className="flex items-center gap-2">
                <Input
                  autoFocus={selection.autoFocusInput}
                  value={question}
                  onChange={(event) => setQuestion(event.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Ask Mate about this…"
                  aria-label="Ask Mate about this…"
                  className="h-9"
                />
                <Button type="button" size="sm" onClick={handleSend}>
                  Ask Mate
                </Button>
              </div>
            </PopoverContent>
          )}
        </Popover>
      )}
    </div>
  )
}
