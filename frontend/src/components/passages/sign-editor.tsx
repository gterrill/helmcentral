import { ChevronDown, Clock, Library, MapPin, Plus, Signpost, Trash2, Upload, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { DocumentLinkPicker, type DocumentLinkPickerResult } from '@/components/inventory/document-link-picker'
import { signPhotoSrc } from '@/components/passages/sign-card'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
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
export type SignChange = Partial<Pick<ReplaySign, 'title' | 'note' | 'visibility' | 'show_s' | 'starts_leg'>>

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

/** One placed sign in the picker row. */
export interface SignChip {
  id: string
  title: string
  /** Boat-local date, time and zone of the sign, for its label. */
  timeLabel: string
  /** What the chip shows: the time, with the date when the signs span days. */
  shortTime: string
  photoSrc: string | null
  selected: boolean
}

// Keyed by the photo source by its caller, so a new photo gets a fresh try.
function ChipThumb({ src }: { src: string }) {
  const [failed, setFailed] = useState(false)
  if (failed) return <Signpost className="h-4 w-4 shrink-0" aria-hidden="true" />
  return <img src={src} alt="" className="h-6 w-6 rounded-sm object-cover" onError={() => setFailed(true)} />
}

function SignChipButton({ chip, disabled, onPick }: { chip: SignChip; disabled: boolean; onPick: (id: string) => void }) {
  return (
    <button
      type="button"
      aria-pressed={chip.selected}
      aria-label={`Edit sign ${chip.title} at ${chip.timeLabel}`}
      disabled={disabled}
      className={cn(
        'flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-1.5 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
        chip.selected ? 'border-primary bg-primary text-primary-foreground' : 'border-border bg-card text-foreground',
      )}
      onClick={() => onPick(chip.id)}
    >
      {chip.photoSrc ? (
        <ChipThumb key={chip.photoSrc} src={chip.photoSrc} />
      ) : (
        <Signpost className="h-4 w-4 shrink-0" aria-hidden="true" />
      )}
      <span className="max-w-40 truncate text-xs font-medium">{chip.title}</span>
      <span className={cn('text-xs tabular-nums', chip.selected ? 'text-primary-foreground' : 'text-muted-foreground')}>{chip.shortTime}</span>
    </button>
  )
}

export interface SignEditorProps {
  /** The sign being edited, or null when none is picked. */
  sign: ReplaySign | null
  /** The placed signs in time order, for the picker row. */
  signs: SignChip[]
  onPick: (id: string) => void
  /** The scrubber is not usable (nothing to replay, or the fly-in is running). */
  disabled?: boolean
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
  /** Moves the sign to the replay cursor. */
  onMoveHere: () => void
  /** The cursor is already on the sign, or there is no cursor. */
  moveHereDisabled: boolean
  /** Moves the sign by this many seconds and seeks there. */
  onNudge: (seconds: number) => void
  /** Moves the sign to the time its photo was taken. */
  onBackToPhotoTime: () => void
}

/** The nudge steps, earliest first. */
const NUDGES: readonly { seconds: number; short: string; words: string }[] = [
  { seconds: -60, short: '\u22121 min', words: '1 minute earlier' },
  { seconds: -10, short: '\u221210 s', words: '10 seconds earlier' },
  { seconds: 10, short: '+10 s', words: '10 seconds later' },
  { seconds: 60, short: '+1 min', words: '1 minute later' },
]

const ACCEPT = SIGN_PHOTO_TYPES.join(',')

export function SignEditor({
  sign, signs, onPick, disabled = false, whenLabel, busy, error, notice, onAdd, onAddPhoto, onAddFromLibrary, onChange, onReplacePhoto, onReplaceFromLibrary, onRemovePhoto, onDelete, onMoveHere, moveHereDisabled, onNudge, onBackToPhotoTime,
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
  const fields = useRef<HTMLDivElement>(null)
  // A join with no name cannot stop being a join until it has one: the switch
  // stays on and the title is asked for.
  const [needsTitle, setNeedsTitle] = useState(false)
  useEffect(() => { setNeedsTitle(false) }, [sign?.id, sign?.starts_leg])
  const toggleLeg = (on: boolean) => {
    if (!sign) return
    if (!on && sign.title === '') {
      setNeedsTitle(true)
      fields.current?.querySelector<HTMLInputElement>('input[aria-label="Sign title"]')?.focus()
      return
    }
    onChange({ starts_leg: on })
  }
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
        {signs.length === 0 && <span className="text-xs text-muted-foreground">No signs yet</span>}
      </div>
      {signs.length > 0 && (
        <div className="flex min-w-0 gap-1 overflow-x-auto pb-1" role="group" aria-label="Signs on this replay">
          {signs.map((c) => <SignChipButton key={c.id} chip={c} disabled={disabled} onPick={onPick} />)}
        </div>
      )}
      {sign && (
        <div className="grid grid-cols-1 gap-x-3 gap-y-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]" data-testid="sign-fields" key={sign.id} ref={fields}>
          <TextField
            label="Sign title"
            value={sign.title}
            max={SIGN_TITLE_MAX}
            required={!sign.starts_leg}
            onCommit={(title) => onChange(needsTitle && title !== '' ? { title, starts_leg: false } : { title })}
          />
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
          <div className="flex min-w-0 flex-wrap items-center gap-2 sm:col-span-2" role="group" aria-label="Move sign in time">
            <Button type="button" size="sm" variant="outline" disabled={moveHereDisabled || disabled} onClick={onMoveHere}>
              <MapPin className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              Move sign here
            </Button>
            <span className="flex flex-wrap gap-1">
              {NUDGES.map((n) => (
                <Button key={n.seconds} type="button" size="sm" variant="outline" title={`Move sign ${n.words}`} disabled={disabled} onClick={() => onNudge(n.seconds)}>
                  {n.short}
                </Button>
              ))}
            </span>
          </div>
          <div className="flex min-w-0 flex-col gap-1 sm:col-span-2">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Switch id="sign-starts-leg" checked={sign.starts_leg} onCheckedChange={toggleLeg} />
              <label htmlFor="sign-starts-leg">Starts a new leg</label>
            </div>
            {sign.starts_leg && sign.title === '' && !needsTitle && (
              <p className="text-xs text-muted-foreground">Awaiting a name from the track. Type one to set it now.</p>
            )}
            {needsTitle && <p role="status" className="text-xs text-muted-foreground">Give this sign a name before it stops starting a leg.</p>}
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
                    <DropdownMenuItem disabled={disabled} onClick={onBackToPhotoTime}>
                      <Clock className="h-4 w-4" aria-hidden="true" />
                      Back to photo time
                    </DropdownMenuItem>
                  )}
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
