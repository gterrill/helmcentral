import { useCallback, useEffect, useRef, useState, type ChangeEvent } from 'react'
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
  Code,
  Code2,
  Heading1,
  Heading2,
  Heading3,
  Image as ImageIcon,
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
import {
  LIST_STYLE_TYPE,
  NOTE_EDITOR_PLUGINS,
  deserializeNoteMarkdown,
  serializeNoteMarkdown,
} from '@/lib/note-editor-config'
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
  const external = /^https?:/i.test(url)
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

// ── the editor itself ────────────────────────────────────────────────────

export default function NoteEditorImpl({ value, onSave, saving = false }: NoteEditorProps) {
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

  const [dirty, setDirty] = useState(false)
  const [sourceMode, setSourceMode] = useState(false)
  const [sourceText, setSourceText] = useState(value)

  // This task's #5 / plan §7: "Opening a note must never write. Dirty state
  // comes from user edits only, never from a serialisation diff." Plate can
  // fire onValueChange once during its own mount-time normalisation (node
  // ids assigned, list structure normalised) - that is Plate tidying up,
  // not an operator edit, so it must not flip the dirty flag. Child
  // components (<Plate>/<PlateContent> below) commit their mount effects
  // before this component's own effect runs, so any such call arrives
  // while mountedRef is still false and is correctly ignored; anything the
  // operator does afterwards runs long after this effect has flipped it.
  const mountedRef = useRef(false)
  useEffect(() => {
    mountedRef.current = true
  }, [])

  const markDirty = useCallback(() => {
    if (!mountedRef.current) return
    setDirty(true)
  }, [])

  // Computed once, from the value this component was constructed with, not
  // from anything the operator has since done - "announced once" (plan §7):
  // a note authored elsewhere (imported .md, hand-edited off a backup) will
  // be reformatted the first time this editor saves it. Comparing here,
  // against the FRESHLY PARSED value, costs no save and no reindex - only
  // an actual Save button press would ever change the stored bytes.
  const [showNormalizedNotice] = useState(() => serializeNoteMarkdown(editor, editor.children) !== value)
  const [saveError, setSaveError] = useState<string | null>(null)

  const handleSave = useCallback(async () => {
    const markdown = sourceMode ? sourceText : serializeNoteMarkdown(editor, editor.children)
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
  }, [editor, onSave, sourceMode, sourceText])

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
    setSourceText(event.target.value)
    markDirty()
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
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
            aria-label="Note markdown source"
            value={sourceText}
            onChange={handleSourceChange}
            className="h-full min-h-60 resize-none font-mono text-xs"
          />
        ) : (
          <Plate editor={editor} onValueChange={markDirty}>
            <PlateContent
              aria-label="Note body"
              placeholder="Write the note…"
              className="min-h-60 text-sm leading-relaxed text-foreground outline-none"
            />
          </Plate>
        )}
      </div>

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

// Inserts a REFERENCE to a document already in the library (plan §5's
// `hc-doc:<uuid>` scheme) - it does not upload anything itself. The
// operator still uploads the photo through Documents first and pastes its
// id here, exactly as docs/how-to/write-the-boats-manual.md's §5 already
// describes; this button removes the "type the Markdown by hand" step, not
// the "upload it first" one, so that how-to page's fallback text stays
// accurate rather than promising an upload flow this control doesn't do.
function ImageButton({ editor }: { editor: PlateEditor }) {
  const [open, setOpen] = useState(false)
  const [docId, setDocId] = useState('')
  const [alt, setAlt] = useState('')
  const [error, setError] = useState<string | null>(null)

  const submit = () => {
    const trimmed = docId.trim()
    const link = resolveNoteHref(`hc-doc:${trimmed}`)
    if (link.kind !== 'document') {
      // Fail loud (AGENTS.md fallback policy): refuse to insert something
      // that isn't a valid document id rather than silently producing a
      // reference that will 404 as "photo missing" later.
      setError('Paste the document id from its Documents address bar (a UUID).')
      return
    }
    editor.tf.insertNodes({
      type: KEYS.img,
      url: link.kind === 'document' ? `hc-doc:${link.id}` : '',
      caption: alt.trim() === '' ? [] : [{ text: alt.trim() }],
      children: [{ text: '' }],
    })
    setDocId('')
    setAlt('')
    setError(null)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
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
          <Input
            autoFocus
            value={docId}
            onChange={(e) => setDocId(e.target.value)}
            placeholder="Document id (from Documents)"
            aria-label="Document id"
          />
          <Input
            value={alt}
            onChange={(e) => setAlt(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') submit() }}
            placeholder="Caption (e.g. Fuel manifold)"
            aria-label="Photo caption"
          />
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
          <Button type="button" size="sm" onClick={submit}>Insert photo</Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
