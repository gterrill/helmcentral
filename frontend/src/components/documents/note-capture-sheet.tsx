import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react'

import { Button } from '@/components/ui/button'
import { DictateButton, DictationError, DictationStatus, useDictation } from '@/components/dictation'
import { NoteEditorBody, type NoteEditorHandle } from '@/components/note-editor'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useNotes, type NoteType } from '@/hooks/use-notes'
import { NOTE_TYPE_META, NOTE_TYPE_ORDER } from '@/lib/note-type-meta'

// ADR 0121: the one capture sheet, reached only through Documents' New →
// Note menu (documents-panel.tsx) - no separate global header action or
// Alt+N shortcut exists any more; every note starts here.
//
// ADR 0124: the body is the full ADR 0117 editor (NoteEditorBody, note-
// editor.tsx), not a plain textarea - headings, tables, links and a task
// list are available from the first word, with the plain-Markdown escape
// hatch (the editor's own "Markdown source" toggle) still one click away
// for whoever wants it. There is no second, plain-field path any more:
// AGENTS.md's fallback policy rules out two authoring surfaces that could
// each silently be the one the operator gets, and that was this ADR's own
// reason for removing the textarea rather than keeping it beside the
// editor as a fallback.
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
  /** The kind the sheet opens on. Documents' New → Note submenu passes one
   * when the operator picked a kind directly rather than Auto; the global
   * header action and Alt+N ADR 0121 removed used to pass nothing, and the
   * submenu's own Auto item still does today. The select stays live either
   * way - this only chooses where it starts. */
  initialType?: NoteType
}

export function NoteCaptureSheet({ open, onOpenChange, onCaptured, initialType }: NoteCaptureSheetProps) {
  const notes = useNotes()
  const editorRef = useRef<NoteEditorHandle | null>(null)
  // Code review: NoteEditorBody is a React.lazy body (note-editor.tsx) - its
  // own "Loading editor…" Suspense fallback can be on screen for a real tick
  // while the editor-vendor chunk fetches, and DictateButton below used to
  // have no idea that window existed at all. `editorReady` tracks the
  // imperative handle's own attach/detach (setEditorHandle below), rather
  // than something inferred from editorKey or a timer, so it is exactly
  // "does editorRef.current point at something right now" - true whenever a
  // call through editorRef is safe, false the instant it wouldn't be.
  const [editorReady, setEditorReady] = useState(false)
  const setEditorHandle = useCallback((handle: NoteEditorHandle | null) => {
    editorRef.current = handle
    setEditorReady(handle !== null)
  }, [])
  const [type, setType] = useState<string>(initialType ?? TYPE_AUTO)
  const [empty, setEmpty] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [capturing, setCapturing] = useState(false)
  // Bumped on every open and after a successful Capture - remounts
  // NoteEditorBody with a fresh, empty value (ADR 0124), the same job
  // setText('') used to do for the plain textarea this replaced. A fresh
  // key rather than an imperative "clear" call because Plate owns its own
  // undo history and node ids per mount; remounting is the one way to
  // guarantee none of a just-captured note's editing history leaks into
  // the next one.
  const [editorKey, setEditorKey] = useState(0)

  // Tracks Capture's disabled state - NoteEditorBody owns the actual
  // buffer (WYSIWYG or source mode), so "is there anything to capture" has
  // to come from what it reports rather than a string this component holds
  // itself.
  //
  // Code review: this used to read editorRef.current?.getMarkdown() instead
  // of taking the string directly - the imperative handle's getMarkdown is
  // a closure over the body's own state as of its LAST completed render,
  // and this was being called from inside the very onChange that fires
  // before that render happens (setSourceText, in source mode). One
  // keystroke landed in the buffer while Capture kept reporting empty.
  // NoteEditorBody's onChange now hands over the fresh Markdown itself
  // (note-editor-impl.tsx), so there is nothing left to read stale.
  const handleBodyChange = useCallback((markdown: string) => {
    setEmpty(markdown.trim() === '')
  }, [])

  const insertDictation = useCallback((text: string) => {
    // Code review: DictateButton's own `disabled={!editorReady}` (below) is
    // meant to make this branch unreachable through the mic itself, but a
    // final can still be in flight from the Web Speech API's own event
    // queue (finish() deliberately leaves the recognizer's handlers attached
    // for exactly this - see use-speech-input.ts's own comment on finish()
    // vs stop()). AGENTS.md's fallback policy: a dictated phrase that can't
    // be delivered has to say so, not vanish - `?.` on editorRef used to let
    // it do exactly that.
    if (!editorRef.current) {
      setError("The editor wasn't ready, so that dictation wasn't added. Try again.")
      return
    }
    editorRef.current.insertDictation(text)
  }, [])

  // ADR 0122: the mic, appended at the editor's caret rather than sent by
  // itself. ADR 0124 moves its sink from the plain field's `setValue` to
  // the editor's own insertion point - see dictation.tsx's own doc comment
  // on the two-branch UseDictationOptions union this made of it.
  const dictation = useDictation({ insert: insertDictation })

  const reset = useCallback(() => {
    setType(initialType ?? TYPE_AUTO)
    setError(null)
    setEmpty(true)
    setEditorKey((key) => key + 1)
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
      setType(initialType ?? TYPE_AUTO)
      setError(null)
      setEmpty(true)
      setEditorKey((key) => key + 1)
    }
  }, [open, initialType])

  const submit = useCallback(async () => {
    const body = (editorRef.current?.getMarkdown() ?? '').trim()
    if (body === '') return
    setCapturing(true)
    try {
      const created = type === TYPE_AUTO
        ? await notes.createNote({ body })
        : await notes.createNote({ body, type: type as NoteType })
      // Code review: a successful Capture closes the sheet but leaves it
      // mounted (this is the one instance, per ADR 0119/0121) - an active
      // dictation left running would keep listening into hidden state and
      // keep holding the voice arbiter's claim, blocking "Hey Mate"
      // indefinitely. Cancelled before reset() so no late result from the
      // just-cancelled session can land in between.
      dictation.cancel()
      reset()
      onOpenChange(false)
      onCaptured?.(created.document.id)
    } catch (err) {
      // AGENTS.md fallback policy: the server's own message surfaces
      // verbatim, and the body stays in the editor so nothing dictated or
      // typed is lost.
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setCapturing(false)
    }
  }, [type, notes, reset, onOpenChange, onCaptured, dictation])

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      void submit()
      return
    }
    dictation.handleFieldKeyDown(event)
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next)
        if (!next) {
          // Code review: Escape, an overlay click and the sheet's own close
          // button all funnel through here - the sheet closes but stays
          // mounted, so a dictation left running would keep listening into
          // state nobody can see and keep holding the voice arbiter's
          // claim. Cancelled before reset() for the same reason submit()
          // above does: no late result can land between the two.
          dictation.cancel()
          reset()
        }
      }}
    >
      {/* sm:max-w-2xl, wider than the sheet used to be (ADR 0124): the
          embedded editor carries a toolbar now, which the old plain
          textarea's sm:max-w-lg never had to leave room for. */}
      <SheetContent side="right" className="flex w-full flex-col gap-4 sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>Capture a note</SheetTitle>
        </SheetHeader>
        {/* ADR 0124: the full editor, not a plain field - see this file's
            own header comment. No Save button or dirty line (NoteEditorBody
            carries neither): Capture below stays the one commit action over
            this buffer. flex-1/min-h-0 come from NoteEditorBody's own root,
            so it grows to fill the space between the header and the rows
            below and scrolls internally rather than pushing Capture off
            the bottom of the sheet on a long note. */}
        <NoteEditorBody
          key={editorKey}
          ref={setEditorHandle}
          value=""
          autoFocus
          placeholder="Write the note…"
          onChange={handleBodyChange}
          onKeyDown={handleKeyDown}
        />
        <div className="flex items-center gap-2">
          <DictateButton dictation={dictation} disabled={!editorReady} />
          <DictationStatus dictation={dictation} />
        </div>
        <DictationError dictation={dictation} />
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
        <div className="flex items-center justify-end gap-2">
          <Button
            type="button"
            size="lg"
            disabled={capturing || empty}
            onClick={() => { void submit() }}
          >
            Capture
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  )
}
