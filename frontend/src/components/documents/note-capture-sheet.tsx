import { Mic } from 'lucide-react'
import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { useNotes, type NoteType } from '@/hooks/use-notes'
import { useSpeechInput } from '@/hooks/use-speech-input'
import { NOTE_TYPE_META, NOTE_TYPE_ORDER } from '@/lib/note-type-meta'
import { cn } from '@/lib/utils'

// ADR 0119: capture is a global action, not a place. This sheet is the ONE
// capture surface for the whole app - opened from App.tsx's header action
// and keyboard shortcut (available from any screen) and from Documents'
// own New → Note menu item (documents-panel.tsx), both handing the exact
// same open/onOpenChange pair down from App.tsx so there is only ever one
// mounted instance regardless of which trigger opened it.
//
// Carries the type select notes-panel.tsx's inbox row never had (that
// panel only ever offered a type OVERRIDE after the fact, via the row's
// icon dropdown) - a synthetic "Auto" default, which is what keeps capture
// itself from blocking on anything: Auto sends no `type` field at all, and
// createNoteHandler (backend/notes_handlers.go) only classifies locally
// (classifyNoteType, no network call) when `type` is absent. Picking an
// explicit type sends it, and the backend records note_type_source:
// 'operator', which SetNoteTypeIfNotOperator then refuses to overwrite -
// see ADR 0119 for why Auto is deliberately NOT itself sent as a value.

const TYPE_AUTO = 'auto'

// The trigger only resolves a value to its item's rendered label once the
// popup's own <SelectItem> list has mounted at least once (Base UI's
// Select.Value looks the value up in whatever it has already registered) -
// so on first render, before the operator has ever opened it, it would
// otherwise show the raw value ("auto") rather than the label ("Auto").
// Passing this map as SelectValue's children function (its own documented
// escape hatch) sidesteps that entirely rather than reaching into the
// shared components/ui/select.tsx wrapper, which every other Select in the
// app uses unchanged.
const TYPE_SELECT_LABELS: Record<string, string> = { [TYPE_AUTO]: 'Auto' }
for (const t of NOTE_TYPE_ORDER) TYPE_SELECT_LABELS[t] = NOTE_TYPE_META[t].label

export interface NoteCaptureSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Called with the freshly captured note's id after a successful POST.
   * Optional - App.tsx doesn't currently navigate to the new note (ADR 0074
   * keeps this sheet out of the URL, and the sheet closes over whatever
   * screen was already open), but a caller that wants to open it can. */
  onCaptured?: (noteId: string) => void
  /** The kind the sheet opens on. Documents' Add Note split button passes
   * one when the operator used the caret to pick a kind up front; the
   * global header action and the split button's own body pass nothing, so
   * the one-click path never meets a capture-time decision (ADR 0119).
   * The select stays live either way - this only chooses where it starts. */
  initialType?: NoteType
}

export function NoteCaptureSheet({ open, onOpenChange, onCaptured, initialType }: NoteCaptureSheetProps) {
  const notes = useNotes()
  const [text, setText] = useState('')
  const [type, setType] = useState<string>(initialType ?? TYPE_AUTO)
  const [error, setError] = useState<string | null>(null)
  const [capturing, setCapturing] = useState(false)

  const speech = useSpeechInput({
    // Appends rather than replaces: a skipper dictating with wet hands may
    // still want to type a correction afterwards, and a second dictation
    // should add to what's already there, not erase it.
    onFinal: (final) => {
      if (final === '') return
      setText((prev) => (prev.trim() === '' ? final : `${prev} ${final}`))
    },
  })

  const reset = useCallback(() => {
    setText('')
    setType(initialType ?? TYPE_AUTO)
    setError(null)
  }, [initialType])

  // Re-seed when the sheet is re-opened on a different kind. The caret can
  // pick Quirk, close, then pick Spec, and a single mounted instance
  // (App.tsx keeps exactly one) would otherwise still be showing Quirk -
  // the useState initialiser above only ever runs once.
  const previousOpenRef = useRef(open)
  useEffect(() => {
    const wasOpen = previousOpenRef.current
    previousOpenRef.current = open
    if (open && !wasOpen) {
      setText('')
      setType(initialType ?? TYPE_AUTO)
      setError(null)
    }
  }, [open, initialType])

  const submit = useCallback(async () => {
    const body = text.trim()
    if (body === '') return
    setCapturing(true)
    try {
      const created = type === TYPE_AUTO
        ? await notes.createNote({ body })
        : await notes.createNote({ body, type: type as NoteType })
      reset()
      onOpenChange(false)
      onCaptured?.(created.document.id)
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message surfaces
      // verbatim, and the text stays in the box so nothing dictated or
      // typed is lost.
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setCapturing(false)
    }
  }, [text, type, notes, reset, onOpenChange, onCaptured])

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      void submit()
    }
  }

  return (
    <Sheet open={open} onOpenChange={(next) => { onOpenChange(next); if (!next) reset() }}>
      <SheetContent side="right" className="flex w-full flex-col gap-4 sm:max-w-lg">
        <SheetHeader>
          <SheetTitle>Capture a note</SheetTitle>
        </SheetHeader>
        <Textarea
          autoFocus
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Capture a note"
          aria-label="Capture a note"
          rows={6}
          className="resize-none"
        />
        {speech.interim !== '' && (
          <p className="text-[11px] text-muted-foreground">{speech.interim}</p>
        )}
        {speech.error && (
          <p role="alert" className="text-[11px] text-destructive">{speech.error}</p>
        )}
        <div className="flex items-center gap-2">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Type</span>
          <Select value={type} onValueChange={(value) => { if (value) setType(value) }}>
            <SelectTrigger aria-label="Note type" className="w-auto min-w-32">
              <SelectValue>{(value: string) => TYPE_SELECT_LABELS[value] ?? value}</SelectValue>
            </SelectTrigger>
            <SelectPopup>
              <SelectItem value={TYPE_AUTO}>Auto</SelectItem>
              {NOTE_TYPE_ORDER.map((t) => (
                <SelectItem key={t} value={t}>{NOTE_TYPE_META[t].label}</SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </div>
        {error && (
          <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="flex items-center justify-between gap-2">
          {/* Same rules as the deleted notes-panel.tsx's capture box: hidden
              entirely with no speech API, shown disabled with an explicit
              reason when the API exists but the page isn't a secure
              context. */}
          {speech.unsupportedReason !== 'no-api' && (
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={speech.listening ? 'Stop dictation' : 'Dictate a note'}
              aria-pressed={speech.listening}
              disabled={speech.unsupportedReason === 'insecure-context'}
              title={speech.unsupportedReason === 'insecure-context' ? 'Voice input needs the app opened over https' : undefined}
              className={cn(speech.listening && 'text-primary')}
              onClick={() => (speech.listening ? speech.stop() : speech.start())}
            >
              <Mic className="h-4 w-4" aria-hidden="true" />
            </Button>
          )}
          <Button
            type="button"
            size="lg"
            className="ml-auto"
            disabled={capturing || text.trim() === ''}
            onClick={() => { void submit() }}
          >
            Capture
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}
