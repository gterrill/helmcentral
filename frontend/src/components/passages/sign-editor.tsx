import { ChevronDown, Library, Plus, Trash2, Upload, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { DocumentLinkPicker, type DocumentLinkPickerResult } from '@/components/inventory/document-link-picker'
import { signPhotoSrc } from '@/components/passages/sign-card'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { isSignPhotoType, SIGN_PHOTO_TYPES } from '@/lib/sign-photo'
import {
  SIGN_NOTE_MAX,
  SIGN_SHOW_MAX,
  SIGN_SHOW_MIN,
  SIGN_TITLE_MAX,
  SIGN_VISIBILITIES,
  type ReplaySign,
} from '@/lib/replay-signs'

/** The editable fields of a sign. */
export type SignChange = Partial<Pick<ReplaySign, 'title' | 'note' | 'visibility' | 'show_s'>>

// Nothing in here is disabled while a write is under way. A field saves when
// it is left, and the click that left it must still land on the button the
// operator was pressing; the writes are queued by the panel and run in order.

interface TextFieldProps {
  label: string
  value: string
  max: number
  multiline?: boolean
  required?: boolean
  onCommit: (value: string) => void
}

// Takes the typed text when the field is left (or on Enter in a single line);
// nothing is sent while typing. The text stays on show until the saved value
// comes back, so a queued save never flashes the old words. An empty required
// field goes back to its value.
function TextField({ label, value, max, multiline = false, required = false, onCommit }: TextFieldProps) {
  const [draft, setDraft] = useState<string | null>(null)
  useEffect(() => { setDraft(null) }, [value])
  const shown = draft ?? value
  const commit = () => {
    if (draft === null) return
    const next = draft.trim()
    if (next === value || (required && next === '')) { setDraft(null); return }
    setDraft(next)
    onCommit(next)
  }
  return (
    <label className="flex min-w-0 flex-col gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      {multiline ? (
        <Textarea
          aria-label={label}
          className="min-h-0 px-2 py-1 text-xs md:text-xs"
          rows={2}
          maxLength={max}
          value={shown}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
        />
      ) : (
        <Input
          aria-label={label}
          className="h-7 px-2 py-0 text-xs md:text-xs"
          maxLength={max}
          value={shown}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
        />
      )}
    </label>
  )
}

function SecondsField({ value, onCommit }: { value: number; onCommit: (value: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null)
  useEffect(() => { setDraft(null) }, [value])
  const commit = () => {
    if (draft === null) return
    const n = Number(draft)
    if (draft.trim() === '' || !Number.isFinite(n)) { setDraft(null); return }
    const next = Math.max(SIGN_SHOW_MIN, Math.min(SIGN_SHOW_MAX, Math.round(n)))
    if (next === value) { setDraft(null); return }
    setDraft(String(next))
    onCommit(next)
  }
  return (
    <label className="flex items-center gap-1 text-xs text-muted-foreground">
      <span>Seconds shown</span>
      <Input
        type="number"
        inputMode="numeric"
        aria-label="Seconds shown"
        className="h-7 w-16 px-1.5 py-0 text-xs tabular-nums md:text-xs"
        min={SIGN_SHOW_MIN}
        max={SIGN_SHOW_MAX}
        step={1}
        value={draft ?? String(value)}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
      />
    </label>
  )
}

export interface SignEditorProps {
  /** The sign being edited, or null when none is picked. */
  sign: ReplaySign | null
  signCount: number
  /** Boat-local time of the sign being edited. */
  whenLabel: string | null
  /** A write is queued or running. Marks the editor busy; nothing is disabled. */
  busy: boolean
  error: string | null
  /** What the last photo told us about where it went: the time used, or why the cursor was. */
  notice: string | null
  onAdd: () => void
  onAddPhoto: (file: File) => void
  onAddFromLibrary: (doc: DocumentLinkPickerResult) => void
  onChange: (change: SignChange) => void
  onReplacePhoto: (file: File) => void
  onReplaceFromLibrary: (doc: DocumentLinkPickerResult) => void
  onRemovePhoto: () => void
  onDelete: () => void
}

const ACCEPT = SIGN_PHOTO_TYPES.join(',')

export function SignEditor({
  sign, signCount, whenLabel, busy, error, notice, onAdd, onAddPhoto, onAddFromLibrary, onChange, onReplacePhoto, onReplaceFromLibrary, onRemovePhoto, onDelete,
}: SignEditorProps) {
  const addInput = useRef<HTMLInputElement>(null)
  const replaceInput = useRef<HTMLInputElement>(null)
  // What the library picker is for: a new sign, or the photo of this one.
  const [picking, setPicking] = useState<'new' | 'replace' | null>(null)
  const [thumbFailed, setThumbFailed] = useState(false)
  const thumb = sign ? signPhotoSrc(sign) : null
  useEffect(() => { setThumbFailed(false) }, [thumb])
  const take = (e: React.ChangeEvent<HTMLInputElement>, then: (file: File) => void) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (file) then(file)
  }
  const hasPhoto = sign?.document_id != null
  return (
    <div className="flex flex-col gap-2 border-t border-border p-2" data-testid="sign-editor" role="group" aria-label="Signs" aria-busy={busy}>
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" variant="outline" onClick={onAdd}>
          <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          Add sign here
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button type="button" size="sm" variant="outline" aria-label="Add photo sign">
                Add photo sign
                <ChevronDown className="h-4 w-4" data-icon="inline-end" aria-hidden="true" />
              </Button>
            }
          />
          <DropdownMenuContent align="start">
            <DropdownMenuItem onClick={() => addInput.current?.click()}>
              <Upload className="h-4 w-4" aria-hidden="true" />
              Upload photo
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setPicking('new')}>
              <Library className="h-4 w-4" aria-hidden="true" />
              From Documents
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <input ref={addInput} type="file" accept={ACCEPT} aria-label="Photo file for a new sign" className="sr-only" tabIndex={-1} onChange={(e) => take(e, onAddPhoto)} />
        <span className="text-xs text-muted-foreground">
          {signCount === 0 ? 'No signs yet' : `${signCount} ${signCount === 1 ? 'sign' : 'signs'}. Pick a mark on the scrubber to edit it`}
        </span>
      </div>
      {sign && (
        <div className="grid grid-cols-1 gap-x-3 gap-y-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]" data-testid="sign-fields" key={sign.id}>
          <TextField label="Sign title" value={sign.title} max={SIGN_TITLE_MAX} required onCommit={(title) => onChange({ title })} />
          <TextField label="Sign note" value={sign.note} max={SIGN_NOTE_MAX} multiline onCommit={(note) => onChange({ note })} />
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
            <span className="flex flex-wrap gap-1" role="group" aria-label="When the sign shows">
              {SIGN_VISIBILITIES.map((v) => (
                <Button
                  key={v.id}
                  type="button"
                  size="sm"
                  variant={sign.visibility === v.id ? 'default' : 'outline'}
                  aria-pressed={sign.visibility === v.id}
                  onClick={() => { if (sign.visibility !== v.id) onChange({ visibility: v.id }) }}
                >
                  {v.label}
                </Button>
              ))}
            </span>
            {sign.visibility === 'passing' && <SecondsField value={sign.show_s} onCommit={(show_s) => onChange({ show_s })} />}
            {whenLabel && <span className="text-xs tabular-nums text-muted-foreground">At {whenLabel}</span>}
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="flex items-center gap-2" role="group" aria-label="Sign photo">
              {thumb && !thumbFailed && (
                <img src={thumb} alt="" className="h-8 w-8 rounded-sm object-cover" data-testid="sign-editor-thumb" onError={() => setThumbFailed(true)} />
              )}
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button type="button" size="sm" variant="outline" aria-label="Photo">
                      {hasPhoto ? 'Photo' : 'No photo'}
                      <ChevronDown className="h-4 w-4" data-icon="inline-end" aria-hidden="true" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="start">
                  <DropdownMenuItem onClick={() => replaceInput.current?.click()}>
                    <Upload className="h-4 w-4" aria-hidden="true" />
                    Upload
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => setPicking('replace')}>
                    <Library className="h-4 w-4" aria-hidden="true" />
                    From Documents
                  </DropdownMenuItem>
                  {hasPhoto && (
                    <DropdownMenuItem onClick={onRemovePhoto}>
                      <X className="h-4 w-4" aria-hidden="true" />
                      Remove
                    </DropdownMenuItem>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
            <input ref={replaceInput} type="file" accept={ACCEPT} aria-label="Photo file for this sign" className="sr-only" tabIndex={-1} onChange={(e) => take(e, onReplacePhoto)} />
            <Button type="button" size="sm" variant="outline" className="ml-auto" onClick={onDelete}>
              <Trash2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Delete sign
            </Button>
          </div>
        </div>
      )}
      {/* Below the fields, so it coming and going never moves a control the operator is pressing. */}
      {notice && <p role="status" className="text-xs text-muted-foreground" data-testid="sign-notice">{notice}</p>}
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      <DocumentLinkPicker
        open={picking !== null}
        onOpenChange={(open) => { if (!open) setPicking(null) }}
        heading="Choose a photo"
        prompt="Search the document library for a photo to use."
        accept={isSignPhotoType}
        rejection="Pick a png, jpeg, gif or webp photo."
        onPick={(doc) => { if (picking === 'new') onAddFromLibrary(doc); else onReplaceFromLibrary(doc) }}
      />
    </div>
  )
}
