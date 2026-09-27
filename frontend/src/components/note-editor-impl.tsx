import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, type ChangeEvent, type KeyboardEvent } from 'react'
import {
  Plate,
  PlateContent,
  PlateElement,
  PlateLeaf,
  useEditorRef,
  usePlateEditor,
  type PlateEditor,
  type PlateElementProps,
  type PlateLeafProps,
} from 'platejs/react'
import { KEYS } from 'platejs'
import { toggleCodeBlock } from '@platejs/code-block'
import { upsertLink } from '@platejs/link'
import { toggleList } from '@platejs/list'
import { insertTable } from '@platejs/table'
import {
  Bold,
  Camera,
  Code,
  Code2,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
  ImagePlus,
  Italic,
  Link as LinkIcon,
  List,
  ListChecks,
  ListOrdered,
  Minus,
  Quote,
  Table as TableIcon,
  type LucideIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Textarea } from '@/components/ui/textarea'
import { apiBaseUrl } from '@/config/api'
import { uploadDocument } from '@/lib/document-upload'
import { downscaleAll, photoFilename } from '@/lib/image-downscale'
import {
  LIST_STYLE_TYPE,
  NOTE_EDITOR_PLUGINS,
  deserializeNoteMarkdown,
  serializeNoteMarkdown,
} from '@/lib/note-editor-config'
import { insertDictatedText } from '@/lib/note-editor-dictation'
import { insertUploadedPhotos, type UploadedPhoto } from '@/lib/note-editor-image-insert'
import { resolveNoteHref } from '@/lib/note-links'
import { cn } from '@/lib/utils'

import type { NoteEditorProps } from './note-editor'

// Plan "Notes and the Boat's Manual" §7 (Phase 2b) / ADR 0117: the actual
// Plate editor. Everything Plate/Slate-shaped lives in this file and
// note-editor-config.ts - note-editor.tsx (the React.lazy wrapper) is the
// only thing anything outside this pair imports.
//
// Radix, named once here rather than argued from scratch at every import
// site: `npm ls @radix-ui/react-slot @radix-ui/react-compose-refs` from
// this dependency install resolves both to ONE path each, both transitive
// through @udecode/react-utils (a platejs dependency), both pure ref/prop
// merging utilities with no portal, focus-trap or dismiss layer of their
// own. ADR 0117's Risk 7 concern - "two focus, portal and dismiss models in
// one app" - does not occur, because neither package implements a focus,
// portal or dismiss model at all. This file and note-editor-config.ts
// import ONLY Plate's headless plugin packages (`platejs`, `@platejs/*`),
// never `@platejs/*-kit` or any Plate prebuilt component - those are the
// Radix-based ones the plan forbids. Every visible control below (the
// toolbar, its Link/Image popovers) is built from this repo's own
// `@/components/ui/*` (Base UI) primitives.

// ── element/leaf renderers ───────────────────────────────────────────────
// @platejs/list's ListPlugin uses the "indent list" model (see
// note-editor-config.ts's own comment): a bulleted/numbered/task list item
// is an ordinary `p` node carrying `indent`/`listStyleType`/`checked`, not a
// nested ul/li tree. So ONE paragraph renderer branches on those three
// fields to draw a bullet, a number or a checkbox - there is no separate
// "list item" element type to register a component for.

function ParagraphElement(props: PlateElementProps) {
  const editor = useEditorRef()
  const element = props.element as unknown as {
    indent?: number
    listStyleType?: string
    listStart?: number
    checked?: boolean
  }
  const indentPad = `${((element.indent ?? 1) - 1) * 1.5}rem`

  if (element.listStyleType === KEYS.listTodo) {
    const checked = Boolean(element.checked)
    return (
      <PlateElement {...props} as="div" className="flex items-start gap-2 py-0.5" style={{ paddingLeft: indentPad }}>
        <span contentEditable={false} className="mt-1 shrink-0 select-none">
          <input
            type="checkbox"
            aria-label="Checklist item"
            checked={checked}
            onChange={() => {
              const path = editor.api.findPath(props.element)
              if (path) editor.tf.setNodes({ checked: !checked }, { at: path })
            }}
            className="h-4 w-4 accent-primary"
          />
        </span>
        <span className={cn('min-w-0 flex-1', checked && 'text-muted-foreground line-through')}>{props.children}</span>
      </PlateElement>
    )
  }

  if (element.listStyleType === KEYS.ul || element.listStyleType === KEYS.ol) {
    const marker = element.listStyleType === KEYS.ol ? `${element.listStart ?? 1}.` : '•'
    return (
      <PlateElement {...props} as="div" className="flex items-start gap-2 py-0.5" style={{ paddingLeft: indentPad }}>
        <span contentEditable={false} className="shrink-0 select-none text-muted-foreground">{marker}</span>
        <span className="min-w-0 flex-1">{props.children}</span>
      </PlateElement>
    )
  }

  return <PlateElement {...props} as="p" className="py-1 leading-relaxed" />
}

function headingClassName(size: 'lg' | 'base' | 'sm') {
  return cn(
    'font-semibold leading-snug text-foreground first:mt-0',
    size === 'lg' && 'mb-2 mt-6 text-lg',
    size === 'base' && 'mb-2 mt-6 text-base',
    size === 'sm' && 'mb-1.5 mt-5 text-sm',
  )
}

function H1Element(props: PlateElementProps) {
  return <PlateElement {...props} as="h1" className={headingClassName('lg')} />
}
function H2Element(props: PlateElementProps) {
  return <PlateElement {...props} as="h2" className={headingClassName('base')} />
}
function H3Element(props: PlateElementProps) {
  return <PlateElement {...props} as="h3" className={headingClassName('sm')} />
}

function BlockquoteElement(props: PlateElementProps) {
  return <PlateElement {...props} as="blockquote" className="border-l-2 border-border py-0.5 pl-3 italic text-muted-foreground" />
}

function HrElement(props: PlateElementProps) {
  return (
    <PlateElement {...props} as="div" className="my-4">
      <hr contentEditable={false} className="border-border" />
      {props.children}
    </PlateElement>
  )
}

function CodeBlockElement(props: PlateElementProps) {
  return <PlateElement {...props} as="pre" className="my-2 overflow-auto rounded-md border border-border bg-muted/40 p-3 font-mono text-xs" />
}
function CodeLineElement(props: PlateElementProps) {
  return <PlateElement {...props} as="div" />
}

function TableElement(props: PlateElementProps) {
  return (
    <PlateElement {...props} as="table" className="my-2 w-full border-collapse text-sm">
      <tbody>{props.children}</tbody>
    </PlateElement>
  )
}
function TableRowElement(props: PlateElementProps) {
  return <PlateElement {...props} as="tr" className="border-b border-border" />
}
function TableCellElement(props: PlateElementProps) {
  return <PlateElement {...props} as="td" className="border border-border px-2 py-1 align-top" />
}
function TableCellHeaderElement(props: PlateElementProps) {
  return <PlateElement {...props} as="th" className="border border-border bg-muted/40 px-2 py-1 text-left align-top font-semibold" />
}

function LinkElement(props: PlateElementProps) {
  const url = (props.element as unknown as { url?: string }).url ?? ''
  // Through resolveNoteHref, like every other point on this boundary: the
  // reader's own anchors, both image renderers, and LinkButton's insert
  // check. A note body is not all operator-typed - it can be a hand-edited
  // blob off a backup, or one of Mate's answers saved verbatim - and this
  // was the one place a url reached an href unvalidated.
  //
  // An unsafe href renders as inert text, exactly as the reader does it, so
  // the editor and the reader agree about what a link is.
  const link = resolveNoteHref(url)
  if (link.kind === 'unsafe') {
    return <PlateElement {...props} as="span" className="text-muted-foreground" />
  }
  const external = link.kind === 'external'
  return (
    <PlateElement
      {...props}
      as="a"
      className="text-primary underline underline-offset-2"
      attributes={{
        ...props.attributes,
        href: url,
        target: external ? '_blank' : undefined,
        rel: external ? 'noreferrer' : undefined,
      }}
    />
  )
}

// Same trust boundary as note-markdown-impl.tsx / ADR 0116: `hc-doc:<uuid>`
// is the only src shape rewritten to a real, same-origin <img>. A note
// hand-edited outside Helmcentral could carry anything in `url` - the
// editor is not exempt from the "no attacker-chosen outbound request"
// argument just because it's editing the operator's own note.
function ImageElement(props: PlateElementProps) {
  const element = props.element as unknown as { url?: string; caption?: { text?: string }[] }
  const link = resolveNoteHref(element.url ?? '')
  const alt = (element.caption ?? []).map((c) => c.text ?? '').join('')

  return (
    <PlateElement {...props} as="div" className="my-2">
      {link.kind === 'document' ? (
        <img
          src={`${apiBaseUrl}/api/documents/${link.id}/content`}
          alt={alt}
          contentEditable={false}
          className="max-w-full rounded-md border border-border"
        />
      ) : (
        <span contentEditable={false} className="text-muted-foreground">{alt || 'Photo missing'}</span>
      )}
      {props.children}
    </PlateElement>
  )
}

function BoldLeaf(props: PlateLeafProps) {
  return <PlateLeaf {...props} as="strong" />
}
function ItalicLeaf(props: PlateLeafProps) {
  return <PlateLeaf {...props} as="em" />
}
function CodeLeaf(props: PlateLeafProps) {
  return <PlateLeaf {...props} as="code" className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]" />
}

// Keyed by plugin key (KEYS.*), the alternative `createPlateEditor` itself
// documents to `.withComponent()` chaining - one map here rather than
// threading `.withComponent()` through NOTE_EDITOR_PLUGINS keeps that
// module import-only-what-round-trip-needs (it's shared with the headless
// test corpus, which has no use for any of this).
const NOTE_EDITOR_COMPONENTS = {
  [KEYS.p]: ParagraphElement,
  [KEYS.h1]: H1Element,
  [KEYS.h2]: H2Element,
  [KEYS.h3]: H3Element,
  [KEYS.bold]: BoldLeaf,
  [KEYS.italic]: ItalicLeaf,
  [KEYS.code]: CodeLeaf,
  [KEYS.blockquote]: BlockquoteElement,
  [KEYS.hr]: HrElement,
  [KEYS.table]: TableElement,
  [KEYS.tr]: TableRowElement,
  [KEYS.td]: TableCellElement,
  [KEYS.th]: TableCellHeaderElement,
  [KEYS.a]: LinkElement,
  [KEYS.img]: ImageElement,
  [KEYS.codeBlock]: CodeBlockElement,
  [KEYS.codeLine]: CodeLineElement,
}

// ── the editor body ──────────────────────────────────────────────────────
// ADR 0124: split out of what used to be the whole of NoteEditorImpl, so
// note-capture-sheet.tsx can embed the ADR 0117 editor - toolbar, WYSIWYG
// surface, Markdown source escape hatch, the lot - without its Save button
// and dirty line. Capture keeps exactly one commit action (its own Capture
// button); a second, disconnected Save inside the body would be a second
// unsaved buffer over the same note, which is the ambiguity a helm screen
// cannot afford. NoteEditorImpl below is now this plus a footer.

export interface NoteEditorBodyProps {
  /** The note's current Markdown body - see NoteEditorProps.value's own
   * doc comment (note-editor.tsx) for why this is read once, at
   * construction, and not reacted to afterwards. */
  value: string
  /** Puts the caret in the body the moment it mounts - the capture sheet
   * wants this (ADR 0124: "the operator presses New Note... and gets the
   * editor, with the caret in the body"); a note reopened for editing does
   * not, since the operator hasn't asked to type yet. */
  autoFocus?: boolean
  /** Fires on an operator edit only - WYSIWYG or source mode - never on
   * mount, never from Plate's own mount-time normalisation. Same mountedRef
   * gate NoteEditorImpl's markDirty used to carry directly; it now lives
   * here so every caller (the footer below, and the capture sheet) gets it
   * for free rather than re-deriving it.
   *
   * No payload (unlike onMarkdownChange below) - this is the sink for a
   * caller that only needs to know an edit happened (NoteEditorImpl's own
   * markDirty), so nothing here has to pay for serialising the Slate
   * document just to hand over a string that gets thrown away. */
  onChange?: () => void
  /** Same edit gate as onChange, but carries the fresh Markdown body - for a
   * caller that actually needs it (the capture sheet's handleBodyChange,
   * tracking whether there's anything to Capture).
   *
   * Code review: this used to be onChange's own payload, computed
   * (serializeNoteMarkdown over the whole Slate document) on every keystroke
   * regardless of whether anything read it - wasted work for
   * NoteEditorImpl's markDirty, which ignores its argument entirely. Kept as
   * a second, optional prop instead of a return value or a ref read: a
   * caller that needs it gets the same "fresh, not stale" guarantee the
   * single onChange used to give (see the note this replaced, still true
   * below at the Plate/source call sites) - `getMarkdown()` off the
   * imperative handle is a closure over state as of the LAST completed
   * render, and reading it from inside the event handler that is itself
   * about to cause the next render reads the value from before that
   * keystroke. */
  onMarkdownChange?: (markdown: string) => void
  /** Forwarded to both the WYSIWYG surface and the source textarea - the
   * capture sheet uses this for Cmd/Ctrl+Enter-to-submit and for
   * useDictation's own Escape-to-cancel handler (dictation.tsx). */
  onKeyDown?: (event: KeyboardEvent<HTMLElement>) => void
  /** WYSIWYG placeholder text. Defaults to the editor's own "Write the
   * note…", unchanged from before this split. */
  placeholder?: string
}

export interface NoteEditorHandle {
  /** The current body as Markdown - sourceText verbatim in source mode
   * (plan §7's escape hatch is authoritative over its own buffer), else
   * through the same serializeNoteMarkdown the footer's own Save uses, so
   * a caller that reads this at Capture time gets byte-for-byte what the
   * editor would have written on Save. */
  getMarkdown(): string
  /** Inserts dictated text at the caret (ADR 0124's mic-in-the-editor
   * decision) - lib/note-editor-dictation.ts's insertDictatedText in
   * WYSIWYG mode, the same append semantics useDictation's `setValue` sink
   * already gives a plain field in source mode (the textarea has no Slate
   * selection to insert at). */
  insertDictation(text: string): void
  /** Focuses whichever surface is currently showing - the WYSIWYG editor,
   * or the source textarea while toggled. */
  focus(): void
}

export const NoteEditorBody = forwardRef<NoteEditorHandle, NoteEditorBodyProps>(function NoteEditorBody(
  { value, autoFocus, onChange, onMarkdownChange, onKeyDown, placeholder },
  ref,
) {
  const editor = usePlateEditor({
    plugins: [...NOTE_EDITOR_PLUGINS],
    components: NOTE_EDITOR_COMPONENTS,
    // A function, not a plain value: this runs AFTER the editor's plugins
    // are registered, which is what deserializeNoteMarkdown needs (it reads
    // plugin-provided type mappings off `editor`). Re-deserialised only
    // once, here, at construction - see note-editor.tsx's own doc comment
    // on NoteEditorProps.value: switching notes remounts this component
    // (the caller keys it by note id) rather than this file reacting to a
    // changed `value` prop on an already-live editor.
    value: (ed) => deserializeNoteMarkdown(ed, value),
  })

  const [sourceMode, setSourceMode] = useState(false)
  const [sourceText, setSourceText] = useState(value)
  const sourceTextareaRef = useRef<HTMLTextAreaElement>(null)

  // This task's #5 / plan §7: "Opening a note must never write. Dirty state
  // comes from user edits only, never from a serialisation diff." Plate can
  // fire onValueChange once during its own mount-time normalisation (node
  // ids assigned, list structure normalised) - that is Plate tidying up,
  // not an operator edit, so it must not reach `onChange`. Child
  // components (<Plate>/<PlateContent> below) commit their mount effects
  // before this component's own effect runs, so any such call arrives
  // while mountedRef is still false and is correctly ignored; anything the
  // operator (or a dictated result, ADR 0124) does afterwards runs long
  // after this effect has flipped it.
  const mountedRef = useRef(false)
  useEffect(() => {
    mountedRef.current = true
  }, [])

  // Takes a thunk, not the markdown itself: the Plate call site below has to
  // serialise the whole Slate document to produce it, and that cost is only
  // worth paying when onMarkdownChange is actually wired up - a caller that
  // only wants onChange (markDirty, which ignores its argument) must not
  // pay for a serialisation nobody reads. handleSourceChange/insertDictation
  // already have their string for free (the textarea's own value), so
  // wrapping it in `() => next` there costs nothing either way.
  const notifyChange = useCallback((getMarkdown: () => string) => {
    if (!mountedRef.current) return
    onChange?.()
    if (onMarkdownChange) onMarkdownChange(getMarkdown())
  }, [onChange, onMarkdownChange])

  // Computed once, from the value this component was constructed with, not
  // from anything the operator has since done - "announced once" (plan §7):
  // a note authored elsewhere (imported .md, hand-edited off a backup) will
  // be reformatted the first time this editor saves it. Comparing here,
  // against the FRESHLY PARSED value, costs no save and no reindex - only
  // an actual Save (or Capture) would ever change the stored bytes. An
  // empty new note (capture's own starting value) serialises to '' both
  // ways, so this stays false there rather than greeting an untouched note
  // with a notice about content it doesn't have.
  const [showNormalizedNotice] = useState(() => serializeNoteMarkdown(editor, editor.children) !== value)

  const toggleSourceMode = useCallback(() => {
    if (sourceMode) {
      // The textarea is authoritative (plan §7's escape hatch): whatever is
      // typed there replaces the WYSIWYG value wholesale, no merge.
      const next = deserializeNoteMarkdown(editor, sourceText)
      editor.tf.setValue(next)
      setSourceMode(false)
    } else {
      setSourceText(serializeNoteMarkdown(editor, editor.children))
      setSourceMode(true)
    }
  }, [editor, sourceMode, sourceText])

  const handleSourceChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
    const next = event.target.value
    setSourceText(next)
    notifyChange(() => next)
  }

  useImperativeHandle(ref, () => ({
    getMarkdown: () => (sourceMode ? sourceText : serializeNoteMarkdown(editor, editor.children)),
    insertDictation: (text: string) => {
      if (sourceMode) {
        // Same append semantics as useDictation's own `setValue` sink
        // (dictation.tsx) - the textarea has no Slate selection to insert
        // at, so this mirrors what a plain field already does rather than
        // inventing a second rule. Computed against `sourceText` directly
        // (not a functional setState update) so the same string can also
        // go to notifyChange - this closure is already rebuilt whenever
        // `sourceText` changes (it's in this useImperativeHandle's own
        // deps below), so it's never stale.
        const trimmed = text.trim()
        if (trimmed === '') return
        const next = sourceText.trim() === '' ? trimmed : `${sourceText} ${trimmed}`
        setSourceText(next)
        notifyChange(() => next)
        return
      }
      // insertDictatedText mutates `editor` directly (a Slate transform),
      // which Plate's own onValueChange below already reports through to
      // notifyChange - the same path a toolbar click already takes, so
      // nothing further needs to be wired here.
      insertDictatedText(editor, text)
    },
    focus: () => {
      if (sourceMode) {
        sourceTextareaRef.current?.focus()
      } else {
        editor.tf.focus()
      }
    },
  }), [editor, sourceMode, sourceText, notifyChange])

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      {showNormalizedNotice && (
        <p className="rounded-md border border-border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
          This note was written outside Helmcentral. Saving will tidy its Markdown into this editor's own style -
          the words won't change.
        </p>
      )}

      <Toolbar editor={editor} sourceMode={sourceMode} onToggleSource={toggleSourceMode} />

      <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border p-3">
        {sourceMode ? (
          <Textarea
            ref={sourceTextareaRef}
            aria-label="Note markdown source"
            value={sourceText}
            onChange={handleSourceChange}
            onKeyDown={onKeyDown}
            autoFocus={autoFocus}
            className="h-full min-h-60 resize-none font-mono text-xs"
          />
        ) : (
          <Plate editor={editor} onValueChange={({ value }) => notifyChange(() => serializeNoteMarkdown(editor, value))}>
            <PlateContent
              aria-label="Note body"
              placeholder={placeholder ?? 'Write the note…'}
              onKeyDown={onKeyDown}
              autoFocus={autoFocus}
              className="min-h-60 text-sm leading-relaxed text-foreground outline-none"
            />
          </Plate>
        )}
      </div>
    </div>
  )
})

// ── the editor itself ────────────────────────────────────────────────────
// The footer (Save button, dirty line, save error) composed on top of
// NoteEditorBody - the shape every caller besides note-capture-sheet.tsx
// still gets (the viewer's Edit toggle, manual-folder-view.tsx). Behaviour
// unchanged from before the ADR 0124 split: nothing here reacts to `value`
// changing identity after mount (switching notes remounts this component,
// keyed by the caller), and dirty still only ever comes from NoteEditorBody
// reporting an actual operator edit through onChange.

export default function NoteEditorImpl({ value, onSave, saving = false }: NoteEditorProps) {
  const bodyRef = useRef<NoteEditorHandle>(null)
  const [dirty, setDirty] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const markDirty = useCallback(() => setDirty(true), [])

  const handleSave = useCallback(async () => {
    const markdown = bodyRef.current?.getMarkdown() ?? ''
    try {
      await onSave(markdown)
      setDirty(false)
      setSaveError(null)
    } catch (err) {
      // AGENTS.md fallback policy: surface the caller's own message rather
      // than a generic failure, and leave `dirty` alone - a failed save
      // must not look, to the operator, like a successful one.
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }, [onSave])

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <NoteEditorBody ref={bodyRef} value={value} onChange={markDirty} />

      {saveError && (
        <p role="alert" className="text-xs text-destructive">{saveError}</p>
      )}
      <div className="flex items-center justify-end gap-2">
        {dirty && <span className="text-[11px] text-muted-foreground">Unsaved changes</span>}
        <Button type="button" disabled={!dirty || saving} onClick={() => { void handleSave() }}>
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </div>
  )
}

// ── toolbar ───────────────────────────────────────────────────────────────
// Every control here is a direct editor.tf/editor.api call or a named
// transform from the node package that owns it - never Plate's own
// Toolbar/ToolbarButton components, which live in the Radix-based prebuilt
// kit this feature deliberately does not depend on (ADR 0117). The enabled
// node set (note-editor-config.ts) IS the button list: there is no button
// for anything this task's node set left out (footnotes, reference-style
// links, raw HTML, mentions, comments) because no such transform is ever
// wired up here - see TestNoteEditor_DisabledNodesAreAbsentFromTheToolbar.

interface ToolbarProps {
  editor: PlateEditor
  sourceMode: boolean
  onToggleSource: () => void
}

function Toolbar({ editor, sourceMode, onToggleSource }: ToolbarProps) {
  // toggle() is registered per plugin key (verified against a real editor
  // instance while building this file: editor.tf.bold.toggle,
  // editor.tf.h1.toggle, editor.tf.blockquote.toggle all exist once their
  // plugin is registered) - cast is for the dynamic-key lookup, not for
  // bypassing a type Plate does provide.
  const toggleTf = (key: 'bold' | 'italic' | 'code' | 'h1' | 'h2' | 'h3' | 'blockquote') => () => {
    ;(editor.tf as unknown as Record<string, { toggle: () => void }>)[key].toggle()
  }

  const insertHr = () => {
    editor.tf.insertNodes({ type: KEYS.hr, children: [{ text: '' }] })
  }

  const insertListStyle = (listStyleType: string) => () => {
    toggleList(editor, { listStyleType })
  }

  return (
    <div className="flex flex-wrap items-center gap-0.5 border-b border-border pb-2">
      <ToolbarButton label="Heading 1" icon={Heading1} onClick={toggleTf('h1')} />
      <ToolbarButton label="Heading 2" icon={Heading2} onClick={toggleTf('h2')} />
      <ToolbarButton label="Heading 3" icon={Heading3} onClick={toggleTf('h3')} />
      <ToolbarDivider />
      <ToolbarButton label="Bold" icon={Bold} onClick={toggleTf('bold')} />
      <ToolbarButton label="Italic" icon={Italic} onClick={toggleTf('italic')} />
      <ToolbarButton label="Inline code" icon={Code} onClick={toggleTf('code')} />
      <ToolbarDivider />
      <ToolbarButton label="Bulleted list" icon={List} onClick={insertListStyle(LIST_STYLE_TYPE.bulleted)} />
      <ToolbarButton label="Numbered list" icon={ListOrdered} onClick={insertListStyle(LIST_STYLE_TYPE.numbered)} />
      <ToolbarButton label="Task list" icon={ListChecks} onClick={insertListStyle(LIST_STYLE_TYPE.task)} />
      <ToolbarDivider />
      <ToolbarButton label="Blockquote" icon={Quote} onClick={toggleTf('blockquote')} />
      <ToolbarButton label="Horizontal rule" icon={Minus} onClick={insertHr} />
      <ToolbarButton label="Code block" icon={Code2} onClick={() => toggleCodeBlock(editor)} />
      <ToolbarDivider />
      <ToolbarButton
        label="Table"
        icon={TableIcon}
        onClick={() => insertTable(editor, { rowCount: 2, colCount: 2, header: true })}
      />
      <LinkButton editor={editor} />
      <ImageButton editor={editor} />
      <ToolbarDivider />
      <Button
        type="button"
        variant={sourceMode ? 'secondary' : 'outline'}
        size="sm"
        aria-pressed={sourceMode}
        aria-label="Markdown source"
        onMouseDown={(event) => event.preventDefault()}
        onClick={onToggleSource}
      >
        {'</>'}
      </Button>
    </div>
  )
}

function ToolbarDivider() {
  return <span className="mx-1 h-5 w-px shrink-0 bg-border" aria-hidden="true" />
}

function ToolbarButton({ label, icon: Icon, onClick }: { label: string; icon: LucideIcon; onClick: () => void }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={label}
      title={label}
      // Keeps the editor's own selection alive across the click - without
      // this, focusing the button first collapses/clears the Slate
      // selection the transform above is supposed to act on.
      onMouseDown={(event) => event.preventDefault()}
      onClick={onClick}
    >
      <Icon className="h-4 w-4" aria-hidden="true" />
    </Button>
  )
}

// Takes a URL the operator TYPES, which makes this the one control in the
// editor that can put an arbitrary scheme into a note body. It refuses
// anything resolveNoteHref calls `unsafe` (ADR 0116's allow-list:
// hc-note:, hc-doc:, #anchor, http(s):, mailto:, tel:) rather than
// inserting it.
//
// This is not the XSS defence - note-markdown-impl.tsx is, and it renders
// an unsafe href as inert text whatever gets into the bytes. This is the
// fallback policy (AGENTS.md): accepting `javascript:` here would write a
// link into the note that silently renders as dead text days later, with
// nothing at the point of insertion to tell the operator why. Refusing
// loudly matches what ImageButton already does with a non-UUID id.
function LinkButton({ editor }: { editor: PlateEditor }) {
  const [open, setOpen] = useState(false)
  const [url, setUrl] = useState('')
  const [error, setError] = useState<string | null>(null)

  const submit = () => {
    const trimmed = url.trim()
    if (trimmed === '') return
    if (resolveNoteHref(trimmed).kind === 'unsafe') {
      setError('That link cannot be opened from a note. Use a note or document reference, an https: address, mailto: or tel:.')
      return
    }
    upsertLink(editor, { url: trimmed })
    setUrl('')
    setError(null)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label="Link"
            title="Link"
            onMouseDown={(event) => event.preventDefault()}
            className="inline-flex h-10 w-10 items-center justify-center rounded-md hover:bg-muted"
          >
            <LinkIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        }
      />
      <PopoverContent className="w-72 p-3">
        <div className="flex flex-col gap-2">
          <Input
            autoFocus
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') submit() }}
            placeholder="hc-note:… or https://…"
            aria-label="Link URL"
          />
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <Button type="button" size="sm" onClick={submit}>Insert link</Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

// Inserts a photo at the cursor, one of two ways: a REFERENCE to a document
// already in the library (plan §5's `hc-doc:<uuid>` scheme, pasted by id -
// the button's original, and still fastest, path when the photo is already
// uploaded), or a fresh Take photo/Add from library pick uploaded here and
// then referenced the same way. docs/adr/0116's dated amendment covers why:
// an operator standing over the job photographs it and gets it into the
// note in one step, rather than a trip to Documents first to upload it and
// copy its id back.
//
// Take photo/Add from library reuse lib/image-downscale.ts's downscaleAll -
// the exact same client-side downscale (and HEIC-to-JPEG re-encode) the
// equipment photo row (photo-strip-editor.tsx) runs before ANY picked photo
// reaches the network - then upload through lib/document-upload.ts's
// uploadDocument, the general document upload endpoint
// (documents_handlers.go's uploadDocumentHandler), tagged `photo`. Not
// uploadEquipmentPhoto (use-inventory.ts): that route also links the
// upload to one equipment item, which a note has no equivalent of - see
// document-upload.ts's own header comment.
function ImageButton({ editor }: { editor: PlateEditor }) {
  const [open, setOpen] = useState(false)
  const [docId, setDocId] = useState('')
  const [alt, setAlt] = useState('')
  const [error, setError] = useState<string | null>(null)
  // Code-review finding: distinct from `error` (role="alert", a real
  // failure) - this is the "the spot changed" FYI (insertUploadedPhotos'
  // own movedToEnd), never a failure: the photo is placed, just not where
  // the operator expected. role="status" below, not "alert".
  const [notice, setNotice] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)

  const cameraInputRef = useRef<HTMLInputElement>(null)
  const libraryInputRef = useRef<HTMLInputElement>(null)

  // The editor's selection at the moment the popover opened. Captured
  // explicitly, rather than trusted to still equal `editor.selection` by
  // the time an upload resolves - Take photo/Add from library hand off to
  // the phone's OWN camera/photo-library UI, an interruption long enough
  // (an actual photo capture, not just a popover click) that the editor's
  // selection cannot be assumed to survive it the way it does across the
  // synchronous doc-id submit below (which the pre-existing onMouseDown
  // preventDefault on the trigger, further down, was already enough to
  // protect). insertUploadedPhotos (lib/note-editor-image-insert.ts) is
  // what checks this saved spot is still real before trusting it.
  const savedSelectionRef = useRef<typeof editor.selection>(null)

  // Inserts one image node per photo, in order, at the popover's own saved
  // selection if it's still a real spot in the document, the end of the
  // note otherwise - see lib/note-editor-image-insert.ts's own doc
  // comment (extracted there so the actual insertion/fallback/catch logic
  // is tested headlessly against a real Slate editor, not through jsdom's
  // own limited Slate-selection support).
  // Returns whether the insert actually succeeded - callers only clear the
  // form/close the popover on true, so an insert failure (surfaced via
  // setError below) leaves the operator looking at the same state a failed
  // upload already would, rather than having its own error immediately
  // wiped by a subsequent unconditional setError(null).
  const insertPhotos = (photos: UploadedPhoto[], caption: string): boolean => {
    const result = insertUploadedPhotos(editor, photos, caption, savedSelectionRef.current)
    if (!result.ok) {
      // Code-review finding: this used to be an unhandled rejection - the
      // photo IS already a real Document (the upload succeeded), so it is
      // named here rather than left to look silently lost.
      setError(result.error ?? 'The photo could not be placed in the note.')
      return false
    }
    setNotice(result.movedToEnd
      ? 'The note changed while the camera was open, so the photo was placed at the end of the note instead.'
      : null)
    return true
  }

  const submitDocId = () => {
    const trimmed = docId.trim()
    const link = resolveNoteHref(`hc-doc:${trimmed}`)
    if (link.kind !== 'document') {
      // Fail loud (AGENTS.md fallback policy): refuse to insert something
      // that isn't a valid document id rather than silently producing a
      // reference that will 404 as "photo missing" later.
      setError('Paste the document id from its Documents address bar (a UUID).')
      return
    }
    if (!insertPhotos([{ id: link.id, name: trimmed }], alt)) return
    setDocId('')
    setAlt('')
    setError(null)
    setOpen(false)
  }

  // Shared by the camera input (always exactly one file) and the library
  // input (any number, `multiple`) - both hand off to onFilesPicked's own
  // shape (photo-strip-editor.tsx's own convention: "normalised to a plain
  // array regardless of the input's own multiple-ness"). Every file is
  // tried, in order, even after an earlier one fails (AGENTS.md fallback
  // policy / the same "no silent rollback" the equipment photo row already
  // follows - use-photo-staging.ts's uploadPhotosInOrder) - a successful
  // pick out of a batch still gets inserted rather than being thrown away
  // because a sibling failed. Only a batch with NO successes leaves the
  // note untouched, which is what makes the single-photo (Take photo) case
  // "on failure, insert nothing".
  const handleFilesPicked = async (files: File[]) => {
    if (files.length === 0) return
    setUploading(true)
    setError(null)
    setNotice(null)
    try {
      const outcomes = await downscaleAll(files)
      const uploaded: UploadedPhoto[] = []
      const failures: { name: string; message: string }[] = []

      for (const { file, result } of outcomes) {
        if (!result.ok) {
          failures.push({ name: file.name, message: result.error })
          continue
        }
        try {
          const doc = await uploadDocument(result.blob, photoFilename(file.name), ['photo'])
          uploaded.push({ id: doc.id, name: file.name })
        } catch (err) {
          failures.push({ name: file.name, message: err instanceof Error ? err.message : String(err) })
        }
      }

      const inserted = uploaded.length === 0 || insertPhotos(uploaded, alt)

      if (failures.length > 0) {
        // Fail loud, not a silent partial success: a single-file pick
        // (Take photo) shows the server's own reason verbatim; a multi-file
        // library pick that partly failed says how many, matching the
        // equipment editor's own wording for the identical situation. This
        // takes precedence over insertPhotos' own error (both would only
        // ever line up if an upload half-succeeded AND the insert also
        // failed - the upload failure is the more actionable of the two).
        setError(
          files.length === 1
            ? failures[0].message
            : `${failures.length} of ${files.length} photos didn't upload: ${failures[0].message}`,
        )
        return
      }
      if (!inserted) return

      setDocId('')
      setAlt('')
      setOpen(false)
    } finally {
      setUploading(false)
    }
  }

  const handlePickerChange = (event: ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    // Materialise into a plain array BEFORE resetting the input's value -
    // same order photo-strip-editor.tsx's own handleInputChange uses, and
    // for the same reason: clearing an <input type="file">'s value is what
    // clears its `.files` FileList too (a live view onto the input's own
    // selection, not an independent snapshot), so reading it after the
    // reset risks reading it already emptied. The reset itself still
    // happens, so picking the SAME file a second time in a row fires a
    // change event again (a file input that still holds its previous value
    // never fires 'change' for an identical pick).
    if (files && files.length > 0) void handleFilesPicked(Array.from(files))
    event.target.value = ''
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (uploading) return
        if (next) savedSelectionRef.current = editor.selection
        setOpen(next)
      }}
    >
      <PopoverTrigger
        render={
          <button
            type="button"
            aria-label="Image"
            title="Image"
            onMouseDown={(event) => event.preventDefault()}
            className="inline-flex h-10 w-10 items-center justify-center rounded-md hover:bg-muted"
          >
            <ImageIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        }
      />
      <PopoverContent className="w-72 p-3">
        <div className="flex flex-col gap-2">
          <input
            ref={cameraInputRef}
            type="file"
            accept="image/*"
            capture="environment"
            aria-label="Take photo"
            className="hidden"
            onChange={handlePickerChange}
          />
          <input
            ref={libraryInputRef}
            type="file"
            accept="image/*"
            multiple
            aria-label="Add from library"
            className="hidden"
            onChange={handlePickerChange}
          />
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="min-w-0 flex-1 gap-1.5"
              disabled={uploading}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => cameraInputRef.current?.click()}
            >
              <Camera className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              <span className="truncate">Take photo</span>
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="min-w-0 flex-1 gap-1.5"
              disabled={uploading}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => libraryInputRef.current?.click()}
            >
              <ImagePlus className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              <span className="truncate">Add from library</span>
            </Button>
          </div>
          {uploading && <p className="text-[11px] text-muted-foreground">Uploading…</p>}

          <div className="h-px bg-border" aria-hidden="true" />

          <Input
            value={docId}
            onChange={(e) => setDocId(e.target.value)}
            placeholder="Or paste a document id (from Documents)"
            aria-label="Document id"
            disabled={uploading}
          />
          <Input
            value={alt}
            onChange={(e) => setAlt(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') submitDocId() }}
            placeholder="Caption (e.g. Fuel manifold)"
            aria-label="Photo caption"
            disabled={uploading}
          />
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          {notice && <p role="status" className="text-[11px] text-muted-foreground">{notice}</p>}
          <Button type="button" size="sm" disabled={uploading} onClick={submitDocId}>Insert photo</Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
